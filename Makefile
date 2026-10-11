.PHONY: build frontend run test docker-build docker-run

DOCKER_IMAGE ?= ledger-finance:local

build: frontend
	go build -buildvcs=false -trimpath -o ledger .

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	@DOCKER_IMAGE="$(DOCKER_IMAGE)" sh scripts/docker-run.sh

frontend: package-lock.json
	npm ci
	npm run build

run: build
	./ledger

test:
	go test ./...
	go vet ./...
	node --check static/app.js
	node --test tests/frontend.test.cjs
	node --test tests/docker-run.test.cjs
