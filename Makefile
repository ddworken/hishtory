help:				## Show this help.
	@fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/\\$$//' | sed -e 's/##//'

local-install:			## Build and install hishtory locally from the current directory
	go build; ./hishtory install

forcetest:			## Force running all tests without a test cache
	go clean -testcache
	make test

test:				## Run all tests
	TZ='America/Los_Angeles' HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 gotestsum --packages ./... --rerun-fails=10 --rerun-fails-max-failures=30 --format testname --jsonfile /tmp/testrun.json --post-run-command "go run client/posttest/main.go" -- -p 1 -timeout 90m  

ftest:				## Run a specific test specified via `make ftest FILTER=TestParam/testTui/color`
	go clean -testcache
	HISHTORY_FILTERED_TEST=1 TZ='America/Los_Angeles' HISHTORY_TEST=1 HISHTORY_SKIP_INIT_IMPORT=1 gotestsum --packages ./... --rerun-fails=0 --format testname -- -p 1 -run "$(FILTER)" -timeout 60m

release:			## [ddworken only] Release the latest version on Github
	# Bump the version
	expr `cat VERSION` + 1 > VERSION
	git add VERSION
	git commit -m "Release v0.`cat VERSION`" --no-verify
	git push 
	gh release create v0.`cat VERSION` --generate-notes
	git push && git push --tags

build-static:			## [ddworken only] Build the server for hishtory.dev 
	ssh server "cd ~/code/hishtory/; git pull; docker build --build-arg GOARCH=amd64 --tag gcr.io/dworken-k8s/hishtory-static --file backend/web/caddy/Dockerfile ."

build-api:			## [ddworken only] Build the API for api.hishtory.dev 
	rm hishtory server || true
	docker build --build-arg GOARCH=amd64 --tag gcr.io/dworken-k8s/hishtory-api --file backend/server/Dockerfile .

deploy-static: 			## [ddworken only] Build and deploy the server for hishtory.dev 
deploy-static: build-static
	ssh server "docker push gcr.io/dworken-k8s/hishtory-static"
	ssh monoserver "cd ~/infra/ && docker compose pull hishtory-static && docker compose rm -svf hishtory-static && docker compose up -d hishtory-static"

deploy-api:			## [ddworken only] Build and deploy the API server for api.hishtory.dev 
deploy-api: build-api
	docker push gcr.io/dworken-k8s/hishtory-api
	ssh monoserver "cd ~/infra/ && docker compose pull hishtory-api && docker compose up -d --no-deps hishtory-api"

deploy:				## [ddworken only] Build and deploy all backend services 
deploy: release deploy-static deploy-api
