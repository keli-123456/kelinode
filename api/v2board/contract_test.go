package panel

import (
	"reflect"
	"testing"
)

func TestNodeAPIContractConstants(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"version":             NodeAPIContractVersion,
		"v1.uniproxy.config":  PathV1UniProxyConfig,
		"v1.uniproxy.user":    PathV1UniProxyUser,
		"v1.uniproxy.delta":   PathV1UniProxyUserDelta,
		"v1.uniproxy.push":    PathV1UniProxyPush,
		"v1.uniproxy.alive":   PathV1UniProxyAlive,
		"v1.uniproxy.list":    PathV1UniProxyAliveList,
		"v1.uniproxy.status":  PathV1UniProxyStatus,
		"v2.server.config":    PathV2ServerConfig,
		"v2.server.handshake": PathV2ServerHandshake,
		"v2.server.report":    PathV2ServerReport,
	}

	expected := map[string]string{
		"version":             "2026-04-24",
		"v1.uniproxy.config":  "/api/v1/server/UniProxy/config",
		"v1.uniproxy.user":    "/api/v1/server/UniProxy/user",
		"v1.uniproxy.delta":   "/api/v1/server/UniProxy/user_delta",
		"v1.uniproxy.push":    "/api/v1/server/UniProxy/push",
		"v1.uniproxy.alive":   "/api/v1/server/UniProxy/alive",
		"v1.uniproxy.list":    "/api/v1/server/UniProxy/alivelist",
		"v1.uniproxy.status":  "/api/v1/server/UniProxy/status",
		"v2.server.config":    "/api/v2/server/config",
		"v2.server.handshake": "/api/v2/server/handshake",
		"v2.server.report":    "/api/v2/server/report",
	}

	for name, want := range expected {
		if got := tests[name]; got != want {
			t.Fatalf("%s drifted from panel contract: got %q want %q", name, got, want)
		}
	}
}

func TestUserInfoJSONContractTags(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(UserInfo{})
	expected := map[string]string{
		"Id":          "id",
		"Uuid":        "uuid",
		"SpeedLimit":  "speed_limit",
		"DeviceLimit": "device_limit",
	}

	for field, want := range expected {
		f, ok := typ.FieldByName(field)
		if !ok {
			t.Fatalf("missing field %s on UserInfo", field)
		}
		if got := f.Tag.Get("json"); got != want {
			t.Fatalf("UserInfo.%s json tag drifted: got %q want %q", field, got, want)
		}
	}
}
