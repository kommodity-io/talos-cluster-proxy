# talos-proxy

A lightweight TCP proxy for [Talos Linux](https://www.talos.dev/) clusters. It accepts incoming connections, reads a target address from a binary header, and performs bidirectional byte forwarding to the target. Designed to run on control-plane nodes to proxy Talos API traffic into the cluster.

## Protocol

Each client connection begins with a simple binary header:

| Field           | Size     | Encoding          | Description                         |
|-----------------|----------|-------------------|-------------------------------------|
| Address length  | 4 bytes  | Big-endian uint32 | Length of the target address string |
| Address         | N bytes  | UTF-8 string      | Target in `host:port` format        |

After the header is read, the proxy dials the target and copies bytes in both directions. Half-close is propagated so that either side can signal end-of-stream independently.

## Usage

```sh
talos-proxy [flags]
```

| Flag               | Default    | Description                                                     |
|--------------------|------------|-----------------------------------------------------------------|
| `-listen-address`  | `:50000`   | Address to listen on (`host:port`)                              |
| `-dial-timeout`    | `5s`       | Timeout for dialing target addresses                            |
| `-allowed-cidrs`   | _(empty)_  | Comma-separated list of allowed target CIDRs (empty = allow all)|

### Examples

```sh
# Listen on the default port, allow all targets
talos-proxy

# Restrict targets to a specific subnet
talos-proxy -allowed-cidrs 10.200.0.0/16

# Multiple allowed CIDRs
talos-proxy -allowed-cidrs "10.200.0.0/16,172.20.0.0/16"
```

## Building

Requires Go 1.24+.

```sh
make build       # binary output to bin/talos-proxy
make test        # run tests with race detector
make lint        # run golangci-lint
```

## Container Image

A minimal `scratch`-based container image is published to `ghcr.io/kommodity-io/talos-proxy`.

Build locally:

```sh
make image VERSION=dev
```

## Helm Chart

A Helm chart is included under `charts/talos-proxy/` for deploying into Kubernetes clusters.

```sh
helm install talos-proxy charts/talos-proxy
```

Key values:

| Value              | Default                              | Description                          |
|--------------------|--------------------------------------|--------------------------------------|
| `listenAddress`    | `:50000`                             | Proxy listen address                 |
| `dialTimeout`      | `5s`                                 | Upstream dial timeout                |
| `allowedCIDRs`     | `""`                                 | Comma-separated allowed target CIDRs |
| `image.repository` | `ghcr.io/kommodity-io/talos-proxy`   | Container image repository           |
| `image.tag`        | Chart `appVersion`                   | Container image tag                  |

By default the chart schedules pods on control-plane nodes with the appropriate tolerations.
