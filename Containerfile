FROM golang:1.24-alpine AS build
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /talos-proxy ./cmd/talos-proxy

FROM scratch
COPY --from=build /talos-proxy /talos-proxy
ENTRYPOINT ["/talos-proxy"]
