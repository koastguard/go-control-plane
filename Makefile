.PHONY: build/linux
build/linux:
	GOOS=linux go build -trimpath -o build/linux/cp ./...

.PHONY: build/docker
build/docker: build/linux
	docker build -t kong/bear-kong-cp:latest .

.PHONY: k3d/start
k3d/start:
	k3d cluster create bear-kong-k8s --kubeconfig-switch-context

run/cp: build/docker
	k3d image import --mode=direct --cluster bear-kong-k8s kong/bear-kong-cp:latest
	kubectl apply -f k8s/bear-kong-cp.yaml
	kubectl rollout restart deployment/bear-kong-cp

APP ?= app-1
run/apps:
	kubectl apply -f k8s/app-1.yaml
	kubectl apply -f k8s/app-2.yaml
	kubectl apply -f k8s/app-3.yaml

exec/curl:
	kubectl exec -it deployment/$(APP) -c netshoot  -- curl -v http://127.0.0.1:8001/  -H 'service: app-1'
	kubectl exec -it deployment/$(APP) -c netshoot  -- curl -v http://127.0.0.1:8001/  -H 'service: app-2'
	kubectl exec -it deployment/$(APP) -c netshoot  -- curl -v http://127.0.0.1:8001/  -H 'service: app-3'

exec/envoy/dump:
	kubectl exec -it deployment/$(APP) -c netshoot  -- curl -v http://127.0.0.1:8081/config_dump

exec/envoy/clusters:
	kubectl exec -it deployment/$(APP) -c netshoot  -- curl -v http://127.0.0.1:8081/clusters

exec/envoy/stats:
	kubectl exec -it deployment/$(APP) -c netshoot  -- curl -v http://127.0.0.1:8081/stats

.PHONY: k3d/stop
k3d/stop:
	k3d cluster delete bear-kong-k8s
