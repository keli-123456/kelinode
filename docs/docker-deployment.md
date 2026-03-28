# v2node Docker Deployment Guide

This guide is for running `v2node` in Docker in production.

It assumes:

- the panel API is already reachable
- the node uses `--network host`
- configuration is stored in `/etc/v2node`
- `v2node` will automatically fetch realtime websocket settings from the panel when available

## 1. Recommended Deployment Model

Recommended layout for one node container:

- container mode: `--network host`
- work directory mount: `/opt/v2node/node-<node-id>:/etc/v2node`
- optional local health endpoint: `127.0.0.1:<health-port>`

Why this layout:

- avoids Docker port mapping issues for protocol listeners
- preserves generated `config.yml`, certificates, geo files, and local sync state
- allows easy per-node isolation on the same host

## 2. Minimum Requirements

- Linux server
- Docker installed
- panel URL, node ID, and node token

Recommended:

- use a dedicated directory per node
- set custom DNS for the container
- use a distinct health port only when you actually need local probes

## 3. Single-Node Docker Run Example

```bash
docker rm -f v2node_hysteria_32 2>/dev/null || true

mkdir -p /opt/v2node/node-32

docker run -d \
  --name v2node_hysteria_32 \
  --restart unless-stopped \
  --network host \
  --dns 1.1.1.1 \
  --dns 8.8.8.8 \
  -v /opt/v2node/node-32:/etc/v2node \
  -e V2NODE_API_HOST="https://panel.example.com" \
  -e V2NODE_NODE_ID="32" \
  -e V2NODE_API_KEY="your-node-token" \
  -e V2NODE_TIMEOUT="30" \
  -e V2NODE_NODE_CONFIG_DIR="/etc/v2node" \
  -e V2NODE_GOMEMLIMIT="1GiB" \
  -e V2NODE_GOGC="100" \
  ghcr.io/keli-123456/v2node:main
```

## 4. Optional Health Check Port

`V2NODE_HEALTH_PORT` is optional.

Only set it when you need local probes such as:

- `curl http://127.0.0.1:<port>/healthz`
- Docker or external local monitoring checks

Example:

```bash
-e V2NODE_HEALTH_PORT="55032"
```

Important:

- if multiple node containers run on the same host with `--network host`, each one must use a unique health port
- if you do not need local health probing, it is safer to leave this unset

## 5. Certificate Download Example

If you distribute node certificates through remote URLs:

```bash
docker run -d \
  --name v2node_hysteria_32 \
  --restart unless-stopped \
  --network host \
  --dns 1.1.1.1 \
  --dns 8.8.8.8 \
  -v /opt/v2node/node-32:/etc/v2node \
  -e V2NODE_API_HOST="https://panel.example.com" \
  -e V2NODE_NODE_ID="32" \
  -e V2NODE_API_KEY="your-node-token" \
  -e V2NODE_TIMEOUT="30" \
  -e V2NODE_NODE_CONFIG_DIR="/etc/v2node" \
  -e V2NODE_GOMEMLIMIT="1GiB" \
  -e V2NODE_GOGC="100" \
  -e V2NODE_TLS_CERT_URL="https://example.com/certificate.crt" \
  -e V2NODE_TLS_KEY_URL="https://example.com/private.key" \
  ghcr.io/keli-123456/v2node:main
```

These files will be stored under the mounted work directory.

## 6. Generated Config Behavior

When `V2NODE_CONFIG_PATH` is not explicitly set:

- Docker entrypoint defaults to `/etc/v2node/config.yml`
- if `config.yml` does not exist, it will generate YAML v2 from environment variables
- if only old `config.json` exists, it still remains compatible

This means Docker deployments do not need a manual config file for the first boot.

## 7. Runtime Tuning Recommendations

Recommended initial values:

- small/medium nodes: `V2NODE_GOMEMLIMIT=512MiB`
- large nodes: `V2NODE_GOMEMLIMIT=1GiB`
- `V2NODE_GOGC=100`

Notes:

- `GoMemLimit` is a soft runtime target, not a hard kill limit
- choose a higher value for large nodes with many active users or heavier protocols

## 8. Realtime Websocket

You do not need to manually set the realtime websocket URL on the node.

`v2node` will get this from the panel config response automatically:

- panel realtime enabled
- public websocket URL correctly configured on the panel side
- reverse proxy for `/ws/node` already working

Expected node logs:

```text
Realtime websocket connected
Realtime websocket authenticated
```

## 9. Verification Commands

### 9.1 Check Node Logs

```bash
docker logs -f v2node_hysteria_32
```

### 9.2 Check Health Endpoint

If `V2NODE_HEALTH_PORT` is set:

```bash
curl http://127.0.0.1:55032/healthz
curl http://127.0.0.1:55032/readyz
```

Expected result:

```text
ok
```

### 9.3 Check Generated Files

```bash
ls -lah /opt/v2node/node-32
```

Common files:

- `config.yml`
- `geoip.dat`
- `geosite.dat`
- certificates
- local sync state files

## 10. Multiple Nodes on the Same Host

Use one work directory per node:

- `/opt/v2node/node-32`
- `/opt/v2node/node-33`
- `/opt/v2node/node-34`

If you use health endpoints, give each one a unique port:

- node 32: `55032`
- node 33: `55033`
- node 34: `55034`

Do not share the same work directory between multiple node containers.

## 11. Updating the Container

```bash
docker pull ghcr.io/keli-123456/v2node:main
docker rm -f v2node_hysteria_32
```

Then rerun the same `docker run` command with the existing mounted work directory.

Because `/etc/v2node` is mounted from the host:

- config
- geo files
- certificates
- local state

will remain after container recreation.

## 12. Common Problems

### DNS Resolution Failure

If logs show:

```text
lookup panel.example.com ... no such host
```

set container DNS explicitly:

```bash
--dns 1.1.1.1 --dns 8.8.8.8
```

### Websocket Connect Timeout

Usually check:

- panel websocket service is running
- reverse proxy `/ws/node` is correct
- public websocket URL is correct
- Cloudflare is not using `Flexible`

### Health Port Conflict

If logs show the health port is already in use:

- remove `V2NODE_HEALTH_PORT`, or
- assign a different port for that node

### Geo Files Download on First Boot

On first boot, logs may show:

```text
geoip.dat not found, downloading ...
geosite.dat not found, downloading ...
```

This is normal.

## 13. Related Files

- [README.md](../README.md)
- [config.yml.example](../config.yml.example)
