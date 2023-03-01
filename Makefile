forcetest:
	go clean -testcache
	HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 go test -p 1 -timeout 30m ./...

test:
	HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 go test -p 1 -timeout 30m ./...

ftest:
	go clean -testcache
	HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 go test -v -p 1 -run "$(FILTER)" ./...

acttest:
	act push -j test -e .github/push_event.json --reuse --container-architecture linux/amd64

release:
	# Bump the version
	expr `cat VERSION` + 1 > VERSION
	git add VERSION
	git commit -m "Release v0.`cat VERSION`" --no-verify
	git push 
	gh release create v0.`cat VERSION` --generate-notes
	git push && git push --tags

build-static:
	ssh server "cd ~/code/hishtory/; git pull; docker build -t gcr.io/dworken-k8s/hishtory-static -f backend/web/caddy/Dockerfile ."

build-api:
	rm hishtory server || true
	docker build -t gcr.io/dworken-k8s/hishtory-api -f backend/server/Dockerfile . 

deploy-static: build-static
	ssh server "docker push gcr.io/dworken-k8s/hishtory-static"
	ssh monoserver "cd ~/infra/ && docker compose pull hishtory-static && docker compose rm -svf hishtory-static && docker compose up -d hishtory-static"

deploy-api: build-api
	docker push gcr.io/dworken-k8s/hishtory-api
	ssh monoserver "cd ~/infra/ && docker compose pull hishtory-api && docker compose up -d --no-deps hishtory-api"

deploy: release deploy-static deploy-api

