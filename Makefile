test:
	HISHTORY_TEST=1 go test -p 1 ./...

build:
	go build -o web/landing/www/hishtory-offline clients/local/client.go
	go build -o web/landing/www/hishtory-online clients/remote/client.go
	docker build -t gcr.io/dworken-k8s/hishtory-static -f web/caddy/Dockerfile .
	docker build -t gcr.io/dworken-k8s/hishtory-api -f server/Dockerfile . 

deploy: build 
	docker push gcr.io/dworken-k8s/hishtory-static
#	docker push gcr.io/dworken-k8s/hishtory-api
	kubectl patch deployment hishtory-static -p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"ts\":\"`date|sed -e 's/ /_/g'|sed -e 's/:/-/g'`\"}}}}}}"
#	kubectl patch deployment hishtory-api -p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"ts\":\"`date|sed -e 's/ /_/g'|sed -e 's/:/-/g'`\"}}}}}}"
