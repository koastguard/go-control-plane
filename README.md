# bear-kong-cp

This is a take home exercise which consists of a very simple service mesh.

It consists of 2 things:
1. The apps which consists of 3 deployments (app-1, app-2, app-3). with each pod running 3 containers:
    - an [Envoy](envoyproxy.io) sidecar, With a bootstrap config to connect to the control-plane using [ADS](https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#xds-protocol).
    - [fake-service](https://github.com/nicholasjackson/fake-service) which is the app that will receive requests.
    - [netshoot](https://github.com/nicolaka/netshoot) a utility container to make things easier to debug (there's curl and a lot of other things there).
2. The control-plane which is a single pod running a kubernetes controller and an XDS server to serve the envoy configuration.

You can start everything by doing:

```
make k3d/start
make run/cp
make run/apps
```

Our service mesh is very simple. It registers each service as a separate route with a header `service: <serviceName>`.
All incoming traffic to an app enters by port 8000 and is then redirected to the listening port of the app (9090).
We expose the envoy admin on port 8081 and there's a few targets in the makefile to make inspection simple.

For example you can do:
```
kubectl exec -it deployment/app-2 -c netshoot  -- curl -v http://127.0.0.1:8001/  -H 'service: app-3'
*   Trying 127.0.0.1:8001...
* Connected to 127.0.0.1 (127.0.0.1) port 8001 (#0)
> GET / HTTP/1.1
> Host: 127.0.0.1:8001
> User-Agent: curl/8.0.1
> Accept: */*
> service: app-3
>
< HTTP/1.1 200 OK
< date: Mon, 10 Jul 2023 10:44:46 GMT
< content-length: 255
< content-type: text/plain; charset=utf-8
< x-envoy-upstream-service-time: 1
< server: envoy
<
{
  "name": "app-3",
  "uri": "/",
  "type": "HTTP",
  "ip_addresses": [
    "10.42.0.103"
  ],
  "start_time": "2023-07-10T10:44:46.466957",
  "end_time": "2023-07-10T10:44:46.467097",
  "duration": "140.667µs",
  "body": "Hello World",
  "code": 200
}
* Connection #0 to host 127.0.0.1 left intact
```

You can see what app-3 responded.
The request takes this path:

```
  -------------------------------------              --------------------------------------
  |          app-2                    |              |        app-3                       |
  |  | netshoot  |    | Envoy      |  |              |  | envoy     |    | fake-service | |
  |  | curl :8001| -> | port 8001  |  | -----------> |  | port 8000 | -> | port 9090    | |
  -------------------------------------              --------------------------------------
```

## Exercise

Checkout [QUESTIONS.md](QUESTIONS.md) for the actual exercise.
For each question you can answer inline. If a code change is required it's highly recommended to make a commit for each answer. 
