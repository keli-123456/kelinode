# Node API Contract

v2node consumes the node-facing Xboardpro APIs defined by
`Xboardpro/contracts/node-api/node-api.json`.

Implementation rules:
- Release v2node and Xboardpro as one upgrade set for contract changes.
- Keep one-version compatibility in both directions during rolling upgrades.
- Use constants from `api/v2board/contract.go`; do not inline endpoint paths.
- Keep `NodeAPIContractVersion` aligned with Xboardpro.
- Add optional fields first when extending response payloads.
- Bump the contract version before removing or renaming fields.
- Do not change endpoint paths, required fields, or field meaning without a
  compatibility window and an explicit contract version bump.

The current client depends on these stable response fields:
- `UserInfo`: `id`, `uuid`, `speed_limit`, `device_limit`
- `UserDelta`: `full`, `revision`, `users`, `deleted`, `upsert`
- `AliveMap`: `alive`, `alive_ips`, `mode`
- `RealtimeHandshake`: `websocket.enabled`, `websocket.ws_url`
