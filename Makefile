test:
	HISHTORY_TEST=1 go test -p 1 ./...

build-static:
	go build -o web/landing/www/hishtory-offline clients/local/client.go
	go build -o web/landing/www/hishtory-online clients/remote/client.go
	docker build -t gcr.io/dworken-k8s/hishtory-static -f web/caddy/Dockerfile .

build-api:
	docker build -t gcr.io/dworken-k8s/hishtory-api -f server/Dockerfile . 

deploy-static: build-static
	docker push gcr.io/dworken-k8s/hishtory-static
	kubectl patch deployment hishtory-static -p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"ts\":\"`date|sed -e 's/ /_/g'|sed -e 's/:/-/g'`\"}}}}}}"

deploy-api: build-api
	docker push gcr.io/dworken-k8s/hishtory-api
	kubectl patch deployment hishtory-api -p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"ts\":\"`date|sed -e 's/ /_/g'|sed -e 's/:/-/g'`\"}}}}}}"
