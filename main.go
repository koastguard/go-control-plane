package main

import (
	"context"
	"encoding/json"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	corev1 "k8s.io/api/core/v1"
	k8s_types "k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sync"
	"time"
)

func main() {
	logf.SetLogger(zap.New())

	wg := &sync.WaitGroup{}
	stop := make(chan struct{})
	defer close(stop)

	wg.Add(3)
	ctx, cancel := context.WithCancel(signals.SetupSignalHandler())
	mgr, err := createManager(ctx)
	if err != nil {
		panic(err)
	}
	go func() {
		defer wg.Done()
		err := mgr.Start(ctx)
		if err != nil {
			logf.Log.Error(err, "controller failed")
			cancel()
		}
	}()
	snapshotCache := cache.NewSnapshotCacheWithHeartbeating(ctx, true, cache.IDHash{}, nil, time.Second)
	go func() {
		defer wg.Done()
		err := runXDSServer(ctx, snapshotCache)
		if err != nil {
			logf.Log.Error(err, "xds service failed")
			cancel()
		}
	}()
	// goroutine
	go func() {
		ticker := time.NewTicker(time.Second * 5)
		defer func() {
			ticker.Stop()
			wg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				logf.Log.Info("tick", "time", t)
				configMap := &corev1.ConfigMap{}
				err := mgr.GetClient().Get(ctx, k8s_types.NamespacedName{Namespace: "default", Name: ConfigMapName}, configMap)
				if err != nil {
					logf.Log.Error(err, "Failed retrieving conf!")
				}
				all := []ServiceMeta{}
				err = json.Unmarshal([]byte(configMap.Data["config"]), &all)
				if err != nil {
					logf.Log.Error(err, "failed reading configMap")
				}
				var clusters []types.Resource
				var routes []*route.Route
				for _, s := range all {
					routes = append(routes, &route.Route{
						Name: s.Name,
						Match: &route.RouteMatch{
							Headers: []*route.HeaderMatcher{
								{
									Name: "service",
									HeaderMatchSpecifier: &route.HeaderMatcher_StringMatch{
										StringMatch: &matcherv3.StringMatcher{
											MatchPattern: &matcherv3.StringMatcher_Exact{
												Exact: s.Name,
											},
										},
									},
								},
							},
							PathSpecifier: &route.RouteMatch_Prefix{
								Prefix: "/",
							},
						},
						Action: &route.Route_Route{
							Route: &route.RouteAction{
								ClusterSpecifier: &route.RouteAction_Cluster{
									Cluster: s.Name,
								},
							},
						},
					})
					clusters = append(clusters, &clusterv3.Cluster{
						Name: s.Name,
						LoadAssignment: &endpointv3.ClusterLoadAssignment{
							ClusterName: s.Name,
							Endpoints: []*endpointv3.LocalityLbEndpoints{
								{
									LbEndpoints: []*endpointv3.LbEndpoint{
										{
											HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
												Endpoint: &endpointv3.Endpoint{
													Address: &corev3.Address{
														Address: &corev3.Address_SocketAddress{
															SocketAddress: &corev3.SocketAddress{
																Address: s.Ip,
																PortSpecifier: &corev3.SocketAddress_PortValue{
																	PortValue: uint32(s.Port),
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					})
				}

				snap, err := cache.NewSnapshot(time.Now().String(), map[resource.Type][]types.Resource{
					resource.ListenerType: {},
					resource.ClusterType:  clusters,
					resource.EndpointType: {},
					resource.RouteType: {
						&route.RouteConfiguration{
							Name: "outbound_route",
							VirtualHosts: []*route.VirtualHost{{
								Name:    "mesh",
								Domains: []string{"*"},
								Routes:  routes,
							}},
						},
					},
				})
				if err != nil {
					logf.Log.Error(err, "failed creating snapshot")
				}
				for _, s := range all {
					err = snapshotCache.SetSnapshot(ctx, s.Name, snap)
				}
				if err != nil {
					logf.Log.Error(err, "failed setting snapshot")
				}
			}
		}
	}()

	<-ctx.Done()
	wg.Wait()
}
