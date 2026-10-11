.PHONY: build frontend run test

build: frontend
	go build -buildvcs=false -trimpath -o ledger .

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
