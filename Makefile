BINARY_NAME := talos-proxy
IMAGE_NAME := ghcr.io/kommodity-io/talos-proxy
VERSION			?= $(shell git describe --tags --always)
TREE_STATE      ?= $(shell git describe --always --dirty --exclude='*' | grep -q dirty && echo dirty || echo clean)
COMMIT			?= $(shell git rev-parse HEAD)
BUILD_DATE		?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
SOURCES			:= $(shell find . -name '*.go')
LINTER := bin/golangci-lint

.PHONY: build test lint golangci-lint clean build-image helm-test

# Set up the linter.
golangci-lint: $(LINTER) ## Download golangci-lint locally if necessary.
$(LINTER):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b bin/ v2.9.0

build: bin/talos-proxy

bin/talos-proxy: $(SOURCES)
	go build -o bin/$(BINARY_NAME) ./cmd/talos-proxy

test:
	go test -v -race ./...

lint: $(LINTER) ## Run the linter.
	$(LINTER) run

lint-fix: $(LINTER) ## Run the linter and fix issues.
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
