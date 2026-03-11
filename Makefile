BINARY_NAME := talos-cluster-proxy
IMAGE_NAME := ghcr.io/kommodity-io/talos-cluster-proxy
VERSION			?= $(shell git describe --tags --always)
SOURCES			:= $(shell find . -name '*.go')
LINTER := bin/golangci-lint

.PHONY: build test lint golangci-lint clean build-image helm-test

# Set up the linter.
golangci-lint: $(LINTER) ## Download golangci-lint locally if necessary.
$(LINTER):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b bin/ v2.9.0

build: bin/talos-cluster-proxy

bin/talos-cluster-proxy: $(SOURCES)
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY_NAME) ./cmd/talos-cluster-proxy

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
