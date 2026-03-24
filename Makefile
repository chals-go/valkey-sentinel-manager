.PHONY: build build-manager build-agent clean test vet lint run

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -s -w -X main.version=$(VERSION)
GOFLAGS = -trimpath

build: build-manager build-agent

build-manager:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/sentinel-manager ./cmd/sentinel-manager

build-agent:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/sentinel-agent ./cmd/sentinel-agent

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

# Docker
docker-build:
	docker build -t valkey-sentinel-manager:$(VERSION) .

docker-build-agent:
	docker build -f Dockerfile.agent -t sentinel-agent:$(VERSION) .
