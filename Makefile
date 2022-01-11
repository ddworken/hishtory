test:
	go test -p ./...

build:
	docker build -t gcr.io/dworken-k8s/hishtory-static -f web/caddy/Dockerfile .
	# docker build -t gcr.io/dworken-k8s/hishtory-api -f deploy/api/Dockerfile . 

deploy: build 
	docker push gcr.io/dworken-k8s/hishtory-static
	# docker push gcr.io/dworken-k8s/hishtory-api
	kubectl patch deployment hishtory-static -p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"ts\":\"`date|sed -e 's/ /_/g'|sed -e 's/:/-/g'`\"}}}}}}"
	# kubectl patch deployment cascara-keys -p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"ts\":\"`date|sed -e 's/ /_/g'|sed -e 's/:/-/g'`\"}}}}}}"
