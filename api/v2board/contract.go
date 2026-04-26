package panel

const (
	NodeAPIContractVersion = "2026-04-26"

	PathV1UniProxyConfig    = "/api/v1/server/UniProxy/config"
	PathV1UniProxyUser      = "/api/v1/server/UniProxy/user"
	PathV1UniProxyUserDelta = "/api/v1/server/UniProxy/user_delta"
	PathV1UniProxyPush      = "/api/v1/server/UniProxy/push"
	PathV1UniProxyAlive     = "/api/v1/server/UniProxy/alive"
	PathV1UniProxyAliveList = "/api/v1/server/UniProxy/alivelist"
	PathV1UniProxyStatus    = "/api/v1/server/UniProxy/status"

	PathV2ServerConfig    = "/api/v2/server/config"
	PathV2ServerHandshake = "/api/v2/server/handshake"
	PathV2ServerReport    = "/api/v2/server/report"
	PathV2MachineNodes    = "/api/v2/server/machine/nodes"
	PathV2MachineStatus   = "/api/v2/server/machine/status"

	HeaderResponseFormat  = "X-Response-Format"
	ResponseFormatMsgpack = "msgpack"
	ContentTypeMsgpack    = "application/x-msgpack"
)
