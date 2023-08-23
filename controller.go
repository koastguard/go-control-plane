package main

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ConfigMapName = "mesh-map"
	enabled       = "enabled"
	MeshSelector  = "meshed"
	MeshTimeout   = "mesh-timeout"

	MaxConnectTimeout  = time.Duration(60 * time.Second)
	MinConnectTimeout  = time.Duration(0 * time.Second)
	TimeoutPlaceHolder = time.Duration(-1 * time.Second)
)

func newConf(ns string, cfg string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      ConfigMapName,
			Labels: map[string]string{
				MeshSelector: enabled,
			},
		},
		Data: map[string]string{
			"config": cfg,
		},
	}
}

func createManager(ctx context.Context) (manager.Manager, error) {
	var log = logf.Log.WithName("controller")
	log.Info("Starting controller")

	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{
		MetricsBindAddress: "0",
	})
	if err != nil {
		return nil, err
	}

	selector, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchLabels: map[string]string{
			MeshSelector: enabled,
		},
	})
	if err != nil {
		return nil, err
	}
	err = builder.
		ControllerManagedBy(mgr). // Create the ControllerManagedBy
		For(&corev1.ConfigMap{}, builder.WithPredicates(selector)).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
			if object.GetNamespace() == "kube-system" { // Don't to anything for
				return nil
			}
			return []reconcile.Request{
				{types.NamespacedName{Namespace: object.GetNamespace(), Name: ConfigMapName}},
			}
		})).
		Complete(&MeshConfReconciler{
			Client: mgr.GetClient(),
		})
	return mgr, err
}

// MeshConfReconciler is a simple ControllerManagedBy example implementation.
type MeshConfReconciler struct {
	client.Client
}

type ServiceMeta struct {
	Name           string `json:"name"`
	Ip             string `json:"ip"`
	Port           int32  `json:"port"`
	ConnectTimeout string `json:"timeout"`
}

func (a *MeshConfReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logf.Log.WithName("reconcile").WithValues("namespace", req.Namespace, "name", req.Name)
	log.Info("reconciling")

	services := &corev1.ServiceList{}
	err := a.List(ctx, services, client.InNamespace(req.Namespace), client.MatchingLabels(map[string]string{MeshSelector: enabled}))
	if err != nil {
		return reconcile.Result{}, err
	}

	var srvs []ServiceMeta
	for _, s := range services.Items {
		if s.Spec.ClusterIP == "" || len(s.Spec.Ports) == 0 {
			continue
		}
		svcMeta := newServiceMeta(&s)
		if svcMeta != nil {
			srvs = append(srvs, *svcMeta)
		}
	}
	sort.SliceStable(srvs, func(i, j int) bool {
		return srvs[i].Name < srvs[j].Name
	})

	data, err := json.MarshalIndent(srvs, "", "  ")
	if err != nil {
		return reconcile.Result{}, err
	}

	if !a.servicesMetaAreChanged(ctx, req.NamespacedName, data) {
		log.Info("services meta are not changed, skip configmap update")
		return reconcile.Result{}, nil
	}

	configMap := newConf(req.Namespace, string(data))

	if err = a.Update(ctx, configMap); err != nil && k8s_errors.IsNotFound(err) {
		if cerr := a.Create(ctx, configMap); cerr != nil {
			return reconcile.Result{}, cerr
		}
	} else if err != nil {
		return reconcile.Result{}, err
	}

	log.Info("updated config map")
	return reconcile.Result{}, nil
}

// servicesMetaAreChanged is used for checking services with meshed=enabled label are changed or not
func (a *MeshConfReconciler) servicesMetaAreChanged(ctx context.Context, name types.NamespacedName, newServicesMeta []byte) bool {
	currentCM := &corev1.ConfigMap{}
	if err := a.Get(ctx, name, currentCM, &client.GetOptions{}); err != nil {
		return true
	}

	currentServicesMeta := currentCM.Data["config"]
	if bytes.Equal([]byte(currentServicesMeta), newServicesMeta) {
		return false
	}

	return true
}

// newServiceMeta is used for creating ServiceMeta object based on service.
// the range of mesh-timeout annotation is 0s~60s. if f the set value exceeds the range,
// it will be forcibly set to the boundary value. by the way, if mesh-timeout annotation
// is not configured, -1s is set as a holder.
func newServiceMeta(svc *corev1.Service) *ServiceMeta {
	if svc == nil || svc.Spec.ClusterIP == "" || len(svc.Spec.Ports) == 0 {
		return nil
	}

	connectTimeout := TimeoutPlaceHolder
	if len(svc.Annotations[MeshTimeout]) != 0 {
		timeout, err := time.ParseDuration(svc.Annotations[MeshTimeout])
		if err != nil {
			// invalid mesh timeout, take as a un-configured service
		} else if timeout > MaxConnectTimeout {
			connectTimeout = MaxConnectTimeout
		} else if timeout < MinConnectTimeout {
			connectTimeout = MinConnectTimeout
		} else {
			connectTimeout = timeout
		}
	}

	return &ServiceMeta{
		Name:           svc.Name,
		Ip:             svc.Spec.ClusterIP,
		Port:           svc.Spec.Ports[0].Port,
		ConnectTimeout: connectTimeout.String(),
	}
}
