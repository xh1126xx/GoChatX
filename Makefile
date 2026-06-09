.PHONY: build build-gateway build-authsvc test lint vet clean docker-build docker-up docker-down cert

# ── Build ──

build: build-gateway build-authsvc

build-gateway:
	go build -o bin/gateway ./cmd/gateway

build-authsvc:
	go build -o bin/authsvc ./cmd/authsvc

# ── Quality ──

test:
	go test -race -cover ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# ── Docker ──

docker-build:
	docker build --target authsvc -t gochatx-authsvc .
	docker build --target gateway -t gochatx-gateway .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

# ── TLS Certificate (development) ──

cert:
	./nginx/gen-cert.sh

# ── Clean ──

clean:
	rm -rf bin/
	rm -f authsvc gateway authsvc.exe gateway.exe
