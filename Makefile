.PHONY: build build-manager build-agent clean test vet lint run \
       build-dns-aws build-dns-azure build-dns-cloudflare build-dns-restapi \
       release

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -s -w -X main.version=$(VERSION)
GOFLAGS = -trimpath

build: build-manager build-agent

build-manager:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/sentinel-manager ./cmd/sentinel-manager

build-agent:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/sentinel-agent ./cmd/sentinel-agent

# Selective DNS provider builds (default 'build' includes all providers)
build-dns-aws:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -tags "dns_select,dns_route53" -o bin/sentinel-manager ./cmd/sentinel-manager

build-dns-azure:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -tags "dns_select,dns_azure" -o bin/sentinel-manager ./cmd/sentinel-manager

build-dns-cloudflare:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -tags "dns_select,dns_cloudflare" -o bin/sentinel-manager ./cmd/sentinel-manager

build-dns-restapi:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -tags "dns_select,dns_restapi" -o bin/sentinel-manager ./cmd/sentinel-manager

clean:
	rm -rf bin/

test:
	go test -race ./...

vet:
	go vet ./...

lint: vet
	@which golangci-lint > /dev/null 2>&1 || echo "golangci-lint not installed"
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || true

run: build-manager
	./bin/sentinel-manager -config .env

# Release: build all 4 binaries (linux amd64/arm64)
release:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/sentinel-manager-linux-amd64 ./cmd/sentinel-manager
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/sentinel-manager-linux-arm64 ./cmd/sentinel-manager
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/sentinel-agent-linux-amd64 ./cmd/sentinel-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/sentinel-agent-linux-arm64 ./cmd/sentinel-agent
	@cd dist && sha256sum * > checksums.txt
	@echo "Release binaries built in dist/"

# Docker
docker-build:
	docker build -t valkey-sentinel-manager:$(VERSION) .

docker-build-agent:
	docker build -f Dockerfile.agent -t sentinel-agent:$(VERSION) .
