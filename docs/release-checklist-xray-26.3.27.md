# Xray 26.3.27 Upgrade Release Checklist

Last updated: 2026-03-30

## Scope

This upgrade aligns the project to:

- `wyx2685/v2node@c26bccc4a97779a9842dc9ce4cd0e8eb9d4aafa7`
- `wyx2685/Xray-core@41a3489fe93477daf3231644723ef183b642120f`

Local release points:

- `keli-core@8f394a0cb8dd495ebc8214825c1c8519272834d3`
- `v2node@40c92382e16515f47ca626304af0f8e894b6c404`

Included upstream core changes:

- `f03b44ac` Finalmask noise `randRange`
- `cb5d6031` WireGuard inbound multi-peer and routing fix
- `6ac88bc3` version bump to `v26.3.27`
- `8f394a0c` ANYTLS frame handling refactor

## Pre-Release

Run the following in a Go 1.26+ environment with external module access:

```bash
cd /path/to/keli-core
GOEXPERIMENT=jsonv2 go test ./proxy/anytls ./proxy/wireguard ./transport/internet/finalmask/noise

cd /path/to/v2node
GOEXPERIMENT=jsonv2 go test ./...
```

Confirm these dependency points before packaging:

- `v2node/go.mod` uses `github.com/xtls/xray-core v1.260327.0`
- `v2node/go.mod` replace points to `github.com/keli-123456/keli-core v0.0.0-20260330084600-8f394a0cb8dd`
- `v2node/go.sum` contains the matching `keli-core` checksum

## Deployment

Release in this order:

1. Build and publish the new `keli-core` dependency.
2. Build and publish the new `v2node` binary or image.
3. Upgrade one canary node first.
4. Verify canary for at least one traffic cycle before broad rollout.

For Docker nodes:

- keep the existing mounted `/etc/v2node` work directory
- do not share one work directory across multiple node containers
- only set `V2NODE_HEALTH_PORT` if you actually probe it

## Functional Checks

After canary deployment, verify all of the following:

1. Process starts normally and no panic appears in `docker logs` or service logs.
2. Health endpoints return `ok` when `V2NODE_HEALTH_PORT` is enabled.
3. Panel config fetch succeeds and node can still pull `/config` and `/user`.
4. Incremental sync still works and `/api/v1/server/UniProxy/user_delta` returns normally.
5. Realtime websocket connects and authenticates.
6. Admin panel realtime status updates as expected.
7. ANYTLS nodes accept new connections and sustain existing traffic.
8. WireGuard nodes with multiple peers still pass traffic correctly.
9. UDP paths that rely on Finalmask noise still behave normally if that feature is in use.

Recommended spot checks:

```bash
docker logs -f <v2node-container>
curl http://127.0.0.1:<health-port>/healthz
curl http://127.0.0.1:<health-port>/readyz
```

Expected realtime log lines:

```text
Realtime websocket connected
Realtime websocket authenticated
```

## Panel-Side Checks

Confirm these panel capabilities are still healthy after rollout:

- node realtime service is enabled and reachable from the node
- reverse proxy for `/ws/node` still works
- admin realtime dashboard still receives node state
- panel route `/api/v1/server/UniProxy/user_delta` remains enabled

## Rollback

Rollback points for this upgrade:

- `v2node`: return from `40c9238` to `b1dcb26`
- `keli-core`: return from `8f394a0c` to `cf3881fd`

Rollback trigger examples:

- ANYTLS handshake failures increase after release
- WireGuard multi-peer traffic becomes unstable
- realtime websocket cannot stay connected
- incremental user sync falls back or starts failing
