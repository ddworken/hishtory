forcetest:
	go clean -testcache
	TZ='America/Los_Angeles' HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 go test -p 1 -timeout 45m ./...

test:
	TZ='America/Los_Angeles' HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 go test -p 1 -timeout 45m ./...

ftest:
	go clean -testcache
	TZ='America/Los_Angeles' HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 go test -v -p 1 -run "$(FILTER)" ./...

retrying-test:
	for i in `seq 1 3`; do echo "Test attempt number $$i"; rm -rf ~/.hishtory/; make test && break; done

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
	ssh server "cd ~/code/hishtory/; git pull; docker build --build-arg GOARCH=amd64 --tag gcr.io/dworken-k8s/hishtory-static --file backend/web/caddy/Dockerfile ."

build-api:
	rm hishtory server || true
	docker build --build-arg GOARCH=amd64 --tag gcr.io/dworken-k8s/hishtory-api --file backend/server/Dockerfile .

deploy-static: build-static
	ssh server "docker push gcr.io/dworken-k8s/hishtory-static"
	ssh monoserver "cd ~/infra/ && docker compose pull hishtory-static && docker compose rm -svf hishtory-static && docker compose up -d hishtory-static"

deploy-api: build-api
	docker push gcr.io/dworken-k8s/hishtory-api
	ssh monoserver "cd ~/infra/ && docker compose pull hishtory-api && docker compose up -d --no-deps hishtory-api"

deploy: release deploy-static deploy-api

