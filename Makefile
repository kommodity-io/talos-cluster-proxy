BINARY_NAME := talos-cluster-proxy
IMAGE_NAME := ghcr.io/kommodity-io/talos-cluster-proxy
VERSION			?= $(shell git describe --tags --always)
SOURCES			:= $(shell find . -name '*.go')

# Set up the linter. Version pinned via `tool` directive in go.mod.
LINTER := go tool golangci-lint

.PHONY: build test lint golangci-lint clean build-image helm-test

build: bin/talos-cluster-proxy

bin/talos-cluster-proxy: $(SOURCES)
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY_NAME) ./cmd/talos-cluster-proxy

test:
	go test -v -race ./...

lint: ## Run the linter.
	$(LINTER) run

lint-fix: ## Run the linter and fix issues.
	$(LINTER) run --fix

clean:
	rm -rf bin/

build-image:
	docker buildx build \
	-f Containerfile \
	-t $(IMAGE_NAME):$(VERSION) \
	--build-arg VERSION=$(VERSION) \
	--load \
	.

helm-test:
	helm unittest charts/*
