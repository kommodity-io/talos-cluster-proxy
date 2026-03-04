FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
RUN apk add --no-cache make git
ARG TARGETOS
ARG TARGETARCH
ARG VERSION

WORKDIR /app
COPY . .
RUN go env -w GOCACHE=/go-cache
RUN go env -w GOMODCACHE=/gomod-cache

RUN go mod download

RUN --mount=type=cache,target=/gomod-cache --mount=type=cache,target=/go-cache \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} VERSION=${VERSION} make build

FROM scratch
COPY --from=build /app/bin/talos-proxy /talos-proxy
ENTRYPOINT ["/talos-proxy"]
