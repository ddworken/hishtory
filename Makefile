forcetest:
	go clean -testcache
	make test

test:
	TZ='America/Los_Angeles' HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 gotestsum --packages ./... --rerun-fails=10 --rerun-fails-max-failures=30 --format testname --jsonfile /tmp/testrun.json --post-run-command "go run client/test_metrics_exporter/main.go" -- -p 1 -timeout 90m  

ftest:
	go clean -testcache
	HISHTORY_FILTERED_TEST=1 TZ='America/Los_Angeles' HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 gotestsum --packages ./... --rerun-fails=0 --format testname -- -p 1 -run "$(FILTER)" -timeout 60m

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

