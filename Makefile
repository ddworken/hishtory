forcetest:
	go clean -testcache
	HISHTORY_TEST=1 go test -p 1 ./...

test:
	HISHTORY_TEST=1 go test -p 1 ./...

acttest:
	act push -j test

release:
	# Bump the version
	expr `cat VERSION` + 1 > VERSION
	git add VERSION
	git commit -m "Release: start releasing v0.`cat VERSION`" --no-verify
	# Release linux-amd64
	cp .slsa-goreleaser-linux-amd64.yml .slsa-goreleaser.yml 
	git add .slsa-goreleaser.yml 
	git commit -m "Release linux-amd64 v0.`cat VERSION`" --no-verify
	git tag v0.`cat VERSION`-linux-amd64
	# Release darwin-amd64
	cp .slsa-goreleaser-darwin-amd64.yml .slsa-goreleaser.yml 
	git add .slsa-goreleaser.yml 
	git commit -m "Release darwin-amd64 v0.`cat VERSION`" --no-verify
	git tag v0.`cat VERSION`-darwin-amd64
	# Release darwin-arm64
	cp .slsa-goreleaser-darwin-arm64.yml .slsa-goreleaser.yml 
	git add .slsa-goreleaser.yml 
	git commit -m "Release darwin-arm64 v0.`cat VERSION`" --no-verify
	git tag v0.`cat VERSION`-darwin-arm64
	# Clean up by removing .slsa-goreleaser.yml 
	rm .slsa-goreleaser.yml 
	git add .slsa-goreleaser.yml 
	git commit -m "Release: finish releasing v0.`cat VERSION`" --no-verify
	# Push to trigger the releases
	#git push
	#git push --tags

build-static:
	docker build -t gcr.io/dworken-k8s/hishtory-static -f backend/web/caddy/Dockerfile .

build-api:
	docker build -t gcr.io/dworken-k8s/hishtory-api -f backend/server/Dockerfile . 

deploy-static: build-static
	docker push gcr.io/dworken-k8s/hishtory-static
	kubectl patch deployment hishtory-static -p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"ts\":\"`date|sed -e 's/ /_/g'|sed -e 's/:/-/g'`\"}}}}}}"

deploy-api: build-api
	docker push gcr.io/dworken-k8s/hishtory-api
	kubectl patch deployment hishtory-api -p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"ts\":\"`date|sed -e 's/ /_/g'|sed -e 's/:/-/g'`\"}}}}}}"

deploy: release deploy-static deploy-api

