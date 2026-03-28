package core

import (
	"encoding/json"
	"testing"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/format"
	coreConf "github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy/anytls"
	hyaccount "github.com/xtls/xray-core/proxy/hysteria/account"
	"github.com/xtls/xray-core/proxy/tuic"
)

func TestBuildHysteria2Config(t *testing.T) {
	t.Parallel()

	nodeInfo := &panel.NodeInfo{
		Type: "hysteria2",
		Common: &panel.CommonNode{
			UpMbps:                  100,
			DownMbps:                200,
			Obfs:                    "salamander",
			ObfsPassword:            "test-obfs-password",
			Ignore_Client_Bandwidth: false,
		},
	}

	inbound := &coreConf.InboundDetourConfig{}
	if err := buildHysteria2(nodeInfo, inbound); err != nil {
		t.Fatalf("build hysteria2 inbound failed: %v", err)
	}

	if inbound.Protocol != "hysteria" {
		t.Fatalf("unexpected protocol: got %q want %q", inbound.Protocol, "hysteria")
	}
	if inbound.StreamSetting == nil || inbound.StreamSetting.Network == nil {
		t.Fatalf("stream settings were not initialized")
	}
	if got, want := *inbound.StreamSetting.Network, coreConf.TransportProtocol("hysteria"); got != want {
		t.Fatalf("unexpected transport: got %q want %q", got, want)
	}
	if inbound.StreamSetting.HysteriaSettings == nil || inbound.StreamSetting.HysteriaSettings.Version != 2 {
		t.Fatalf("unexpected hysteria settings: %+v", inbound.StreamSetting.HysteriaSettings)
	}
	if inbound.StreamSetting.FinalMask == nil || inbound.StreamSetting.FinalMask.QuicParams == nil {
		t.Fatalf("expected brutal final mask to be configured")
	}
	if got, want := inbound.StreamSetting.FinalMask.QuicParams.Congestion, "force-brutal"; got != want {
		t.Fatalf("unexpected congestion control: got %q want %q", got, want)
	}
	if got, want := string(inbound.StreamSetting.FinalMask.QuicParams.BrutalUp), "100mbps"; got != want {
		t.Fatalf("unexpected brutal up bandwidth: got %q want %q", got, want)
	}
	if got, want := string(inbound.StreamSetting.FinalMask.QuicParams.BrutalDown), "200mbps"; got != want {
		t.Fatalf("unexpected brutal down bandwidth: got %q want %q", got, want)
	}
	if got := inbound.StreamSetting.FinalMask.Udp; len(got) != 1 {
		t.Fatalf("unexpected udp mask count: got %d want 1", len(got))
	}
	if got, want := inbound.StreamSetting.FinalMask.Udp[0].Type, "salamander"; got != want {
		t.Fatalf("unexpected obfs type: got %q want %q", got, want)
	}
	if inbound.StreamSetting.FinalMask.Udp[0].Settings == nil {
		t.Fatalf("expected obfs settings")
	}

	var obfsSettings map[string]string
	if err := json.Unmarshal(*inbound.StreamSetting.FinalMask.Udp[0].Settings, &obfsSettings); err != nil {
		t.Fatalf("unmarshal obfs settings failed: %v", err)
	}
	if got, want := obfsSettings["password"], "test-obfs-password"; got != want {
		t.Fatalf("unexpected obfs password: got %q want %q", got, want)
	}

	var settings coreConf.HysteriaServerConfig
	if err := json.Unmarshal(*inbound.Settings, &settings); err != nil {
		t.Fatalf("unmarshal hysteria settings failed: %v", err)
	}
	if settings.Version != 2 {
		t.Fatalf("unexpected hysteria server version: got %d want 2", settings.Version)
	}
}

func TestBuildHysteria2ConfigWithoutFinalMask(t *testing.T) {
	t.Parallel()

	nodeInfo := &panel.NodeInfo{
		Type: "hysteria2",
		Common: &panel.CommonNode{
			Ignore_Client_Bandwidth: true,
		},
	}

	inbound := &coreConf.InboundDetourConfig{}
	if err := buildHysteria2(nodeInfo, inbound); err != nil {
		t.Fatalf("build hysteria2 inbound failed: %v", err)
	}

	if inbound.StreamSetting == nil {
		t.Fatalf("stream settings were not initialized")
	}
	if inbound.StreamSetting.FinalMask != nil {
		t.Fatalf("expected final mask to stay nil when bandwidth override and obfs are disabled")
	}
}

func TestBuildTuicConfig(t *testing.T) {
	t.Parallel()

	nodeInfo := &panel.NodeInfo{
		Type: "tuic",
		Common: &panel.CommonNode{
			CongestionControl: "bbr",
			ZeroRTTHandshake:  true,
		},
	}

	inbound := &coreConf.InboundDetourConfig{}
	if err := buildTuic(nodeInfo, inbound); err != nil {
		t.Fatalf("build tuic inbound failed: %v", err)
	}

	if inbound.Protocol != "tuic" {
		t.Fatalf("unexpected protocol: got %q want %q", inbound.Protocol, "tuic")
	}
	if inbound.StreamSetting == nil || inbound.StreamSetting.Network == nil {
		t.Fatalf("stream settings were not initialized")
	}
	if got, want := *inbound.StreamSetting.Network, coreConf.TransportProtocol("tuic"); got != want {
		t.Fatalf("unexpected transport: got %q want %q", got, want)
	}

	var settings coreConf.TuicServerConfig
	if err := json.Unmarshal(*inbound.Settings, &settings); err != nil {
		t.Fatalf("unmarshal tuic settings failed: %v", err)
	}
	if got, want := settings.CongestionControl, "bbr"; got != want {
		t.Fatalf("unexpected congestion control: got %q want %q", got, want)
	}
	if !settings.ZeroRttHandshake {
		t.Fatalf("expected zero rtt handshake to be enabled")
	}
}

func TestBuildAnyTLSConfigWithWebSocket(t *testing.T) {
	t.Parallel()

	nodeInfo := &panel.NodeInfo{
		Type: "anytls",
		Common: &panel.CommonNode{
			Network:         "ws",
			NetworkSettings: json.RawMessage(`{"host":"edge.example.com","path":"/tls"}`),
			PaddingScheme:   []string{"stop=8", "max=900"},
		},
	}

	inbound := &coreConf.InboundDetourConfig{}
	if err := buildAnyTLS(nodeInfo, inbound); err != nil {
		t.Fatalf("build anytls inbound failed: %v", err)
	}

	if inbound.Protocol != "anytls" {
		t.Fatalf("unexpected protocol: got %q want %q", inbound.Protocol, "anytls")
	}
	if inbound.StreamSetting == nil || inbound.StreamSetting.Network == nil {
		t.Fatalf("stream settings were not initialized")
	}
	if got, want := *inbound.StreamSetting.Network, coreConf.TransportProtocol("ws"); got != want {
		t.Fatalf("unexpected transport: got %q want %q", got, want)
	}
	if inbound.StreamSetting.WSSettings == nil {
		t.Fatalf("expected websocket transport settings")
	}
	if got, want := inbound.StreamSetting.WSSettings.Host, "edge.example.com"; got != want {
		t.Fatalf("unexpected websocket host: got %q want %q", got, want)
	}
	if got, want := inbound.StreamSetting.WSSettings.Path, "/tls"; got != want {
		t.Fatalf("unexpected websocket path: got %q want %q", got, want)
	}

	var settings coreConf.AnyTLSServerConfig
	if err := json.Unmarshal(*inbound.Settings, &settings); err != nil {
		t.Fatalf("unmarshal anytls settings failed: %v", err)
	}
	if got, want := len(settings.PaddingScheme), 2; got != want {
		t.Fatalf("unexpected padding scheme count: got %d want %d", got, want)
	}
}

func TestBuildHysteria2UserAccount(t *testing.T) {
	t.Parallel()

	userInfo := &panel.UserInfo{Uuid: "hysteria-user"}
	user := buildHysteria2User("node-tag", userInfo)

	if got, want := user.Email, format.UserTag("node-tag", "hysteria-user"); got != want {
		t.Fatalf("unexpected user email: got %q want %q", got, want)
	}
	instance, err := user.Account.GetInstance()
	if err != nil {
		t.Fatalf("decode typed account failed: %v", err)
	}
	account, ok := instance.(*hyaccount.Account)
	if !ok {
		t.Fatalf("unexpected hysteria account type: %T", instance)
	}
	if got, want := account.Auth, "hysteria-user"; got != want {
		t.Fatalf("unexpected hysteria auth: got %q want %q", got, want)
	}
}

func TestBuildTuicUserAccount(t *testing.T) {
	t.Parallel()

	userInfo := &panel.UserInfo{Uuid: "tuic-user"}
	user := buildTuicUser("node-tag", userInfo)

	if got, want := user.Email, format.UserTag("node-tag", "tuic-user"); got != want {
		t.Fatalf("unexpected user email: got %q want %q", got, want)
	}
	instance, err := user.Account.GetInstance()
	if err != nil {
		t.Fatalf("decode typed account failed: %v", err)
	}
	account, ok := instance.(*tuic.Account)
	if !ok {
		t.Fatalf("unexpected tuic account type: %T", instance)
	}
	if got, want := account.Uuid, "tuic-user"; got != want {
		t.Fatalf("unexpected tuic uuid: got %q want %q", got, want)
	}
	if got, want := account.Password, "tuic-user"; got != want {
		t.Fatalf("unexpected tuic password: got %q want %q", got, want)
	}
}

func TestBuildAnyTLSUserAccount(t *testing.T) {
	t.Parallel()

	userInfo := &panel.UserInfo{Uuid: "anytls-user"}
	user := buildAnyTLSUser("node-tag", userInfo)

	if got, want := user.Email, format.UserTag("node-tag", "anytls-user"); got != want {
		t.Fatalf("unexpected user email: got %q want %q", got, want)
	}
	instance, err := user.Account.GetInstance()
	if err != nil {
		t.Fatalf("decode typed account failed: %v", err)
	}
	account, ok := instance.(*anytls.Account)
	if !ok {
		t.Fatalf("unexpected anytls account type: %T", instance)
	}
	if got, want := account.Password, "anytls-user"; got != want {
		t.Fatalf("unexpected anytls password: got %q want %q", got, want)
	}
}
