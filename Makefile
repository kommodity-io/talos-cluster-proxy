BINARY_NAME := talos-proxy
IMAGE_NAME := ghcr.io/kommodity-io/talos-proxy
VERSION ?= dev

.PHONY: build test lint clean image helm-test

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY_NAME) ./cmd/talos-proxy

test:
	go test -v -race ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

image:
	docker buildx build \
	-f Containerfile \
	-t $(IMAGE_NAME):$(VERSION) .

helm-test:
	helm unittest charts/*
