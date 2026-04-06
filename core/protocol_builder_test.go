package core

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/format"
	"github.com/xtls/xray-core/app/proxyman"
	coreConf "github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy/anytls"
	hyproxy "github.com/xtls/xray-core/proxy/hysteria"
	hyaccount "github.com/xtls/xray-core/proxy/hysteria/account"
	"github.com/xtls/xray-core/proxy/tuic"
	hytransport "github.com/xtls/xray-core/transport/internet/hysteria"
	tlscfg "github.com/xtls/xray-core/transport/internet/tls"
	websocketcfg "github.com/xtls/xray-core/transport/internet/websocket"
	"google.golang.org/protobuf/proto"
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

func TestBuildInboundHysteria2WithTLS(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeTestCertificateFiles(t)
	nodeInfo := &panel.NodeInfo{
		Type:     "hysteria2",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ListenIP:   "0.0.0.0",
			ServerPort: 443,
			UpMbps:     100,
			DownMbps:   200,
			CertInfo: &panel.CertInfo{
				CertMode:         "file",
				CertFile:         certFile,
				KeyFile:          keyFile,
				RejectUnknownSni: true,
			},
		},
	}

	config, err := buildInbound(nodeInfo, "hysteria-inbound")
	if err != nil {
		t.Fatalf("build inbound failed: %v", err)
	}

	if got, want := config.Tag, "hysteria-inbound"; got != want {
		t.Fatalf("unexpected inbound tag: got %q want %q", got, want)
	}

	receiver := decodeTypedMessage[*proxyman.ReceiverConfig](t, config.ReceiverSettings)
	if got, want := receiver.GetPortList().Range[0].From, uint32(443); got != want {
		t.Fatalf("unexpected receiver port: got %d want %d", got, want)
	}
	if got, want := receiver.GetStreamSettings().GetProtocolName(), "hysteria"; got != want {
		t.Fatalf("unexpected stream protocol: got %q want %q", got, want)
	}
	if got := receiver.GetStreamSettings().GetQuicParams(); got == nil {
		t.Fatalf("expected quic params to be configured")
	}
	if got, want := receiver.GetStreamSettings().GetQuicParams().GetCongestion(), "force-brutal"; got != want {
		t.Fatalf("unexpected quic congestion: got %q want %q", got, want)
	}

	if got := receiver.GetStreamSettings().GetSecuritySettings(); len(got) != 1 {
		t.Fatalf("unexpected security settings count: got %d want 1", len(got))
	}
	tlsConfig := decodeTypedMessage[*tlscfg.Config](t, receiver.GetStreamSettings().GetSecuritySettings()[0])
	if got, want := tlsConfig.GetNextProtocol(), []string{"h3"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected tls alpn: got %v want %v", got, want)
	}
	if !tlsConfig.GetRejectUnknownSni() {
		t.Fatalf("expected reject unknown sni to be enabled")
	}

	hysteriaTransport := decodeTypedMessage[*hytransport.Config](t, receiver.GetStreamSettings().GetTransportSettings()[0].GetSettings())
	if got, want := hysteriaTransport.GetVersion(), int32(2); got != want {
		t.Fatalf("unexpected hysteria transport version: got %d want %d", got, want)
	}

	proxyConfig := decodeTypedMessage[*hyproxy.ServerConfig](t, config.ProxySettings)
	if got := len(proxyConfig.GetUsers()); got != 0 {
		t.Fatalf("expected empty hysteria users in inbound build, got %d", got)
	}
}

func TestBuildInboundTuicWithTLS(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeTestCertificateFiles(t)
	nodeInfo := &panel.NodeInfo{
		Type:     "tuic",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ListenIP:          "0.0.0.0",
			ServerPort:        8443,
			CongestionControl: "bbr",
			ZeroRTTHandshake:  true,
			CertInfo: &panel.CertInfo{
				CertMode: "file",
				CertFile: certFile,
				KeyFile:  keyFile,
			},
		},
	}

	config, err := buildInbound(nodeInfo, "tuic-inbound")
	if err != nil {
		t.Fatalf("build inbound failed: %v", err)
	}

	receiver := decodeTypedMessage[*proxyman.ReceiverConfig](t, config.ReceiverSettings)
	if got, want := receiver.GetStreamSettings().GetProtocolName(), "tuic"; got != want {
		t.Fatalf("unexpected stream protocol: got %q want %q", got, want)
	}
	if got := receiver.GetStreamSettings().GetSecuritySettings(); len(got) != 1 {
		t.Fatalf("unexpected security settings count: got %d want 1", len(got))
	}
	tlsConfig := decodeTypedMessage[*tlscfg.Config](t, receiver.GetStreamSettings().GetSecuritySettings()[0])
	if got, want := tlsConfig.GetNextProtocol(), []string{"h3"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected tls alpn: got %v want %v", got, want)
	}

	proxyConfig := decodeTypedMessage[*tuic.ServerConfig](t, config.ProxySettings)
	if got, want := proxyConfig.GetCongestionControl(), "bbr"; got != want {
		t.Fatalf("unexpected tuic congestion: got %q want %q", got, want)
	}
	if !proxyConfig.GetZeroRttHandshake() {
		t.Fatalf("expected zero rtt handshake to be enabled")
	}
}

func TestBuildInboundTuicWithCustomALPN(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeTestCertificateFiles(t)
	nodeInfo := &panel.NodeInfo{
		Type:     "tuic",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ListenIP:   "0.0.0.0",
			ServerPort: 8443,
			TlsSettings: panel.TlsSettings{
				ALPN: []string{"h3", "h2"},
			},
			CertInfo: &panel.CertInfo{
				CertMode: "file",
				CertFile: certFile,
				KeyFile:  keyFile,
			},
		},
	}

	config, err := buildInbound(nodeInfo, "tuic-inbound")
	if err != nil {
		t.Fatalf("build inbound failed: %v", err)
	}

	receiver := decodeTypedMessage[*proxyman.ReceiverConfig](t, config.ReceiverSettings)
	tlsConfig := decodeTypedMessage[*tlscfg.Config](t, receiver.GetStreamSettings().GetSecuritySettings()[0])
	if got, want := tlsConfig.GetNextProtocol(), []string{"h3", "h2"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected tls alpn: got %v want %v", got, want)
	}
}

func TestBuildInboundAnyTLSWebSocket(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeTestCertificateFiles(t)
	nodeInfo := &panel.NodeInfo{
		Type:     "anytls",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ListenIP:        "0.0.0.0",
			ServerPort:      9443,
			Network:         "ws",
			NetworkSettings: json.RawMessage(`{"host":"edge.example.com","path":"/ws"}`),
			PaddingScheme:   []string{"stop=8", "max=900"},
			CertInfo: &panel.CertInfo{
				CertMode: "file",
				CertFile: certFile,
				KeyFile:  keyFile,
			},
		},
	}

	config, err := buildInbound(nodeInfo, "anytls-inbound")
	if err != nil {
		t.Fatalf("build inbound failed: %v", err)
	}

	receiver := decodeTypedMessage[*proxyman.ReceiverConfig](t, config.ReceiverSettings)
	if got, want := receiver.GetStreamSettings().GetProtocolName(), "websocket"; got != want {
		t.Fatalf("unexpected stream protocol: got %q want %q", got, want)
	}
	transports := receiver.GetStreamSettings().GetTransportSettings()
	if got := len(transports); got != 1 {
		t.Fatalf("unexpected transport settings count: got %d want 1", got)
	}
	if got, want := transports[0].GetProtocolName(), "websocket"; got != want {
		t.Fatalf("unexpected transport config protocol: got %q want %q", got, want)
	}
	wsConfig := decodeTypedMessage[*websocketcfg.Config](t, transports[0].GetSettings())
	if got, want := wsConfig.GetHost(), "edge.example.com"; got != want {
		t.Fatalf("unexpected websocket host: got %q want %q", got, want)
	}
	if got, want := wsConfig.GetPath(), "/ws"; got != want {
		t.Fatalf("unexpected websocket path: got %q want %q", got, want)
	}

	proxyConfig := decodeTypedMessage[*anytls.ServerConfig](t, config.ProxySettings)
	if got, want := proxyConfig.GetPaddingScheme(), "stop=8\nmax=900"; got != want {
		t.Fatalf("unexpected anytls padding scheme: got %q want %q", got, want)
	}
}

func TestBuildInboundAnyTLSWithCustomALPN(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeTestCertificateFiles(t)
	nodeInfo := &panel.NodeInfo{
		Type:     "anytls",
		Security: panel.Tls,
		Common: &panel.CommonNode{
			ListenIP:      "0.0.0.0",
			ServerPort:    9443,
			Network:       "tcp",
			PaddingScheme: []string{"stop=8"},
			TlsSettings: panel.TlsSettings{
				ALPN: []string{"h2", "http/1.1"},
			},
			CertInfo: &panel.CertInfo{
				CertMode: "file",
				CertFile: certFile,
				KeyFile:  keyFile,
			},
		},
	}

	config, err := buildInbound(nodeInfo, "anytls-inbound")
	if err != nil {
		t.Fatalf("build inbound failed: %v", err)
	}

	receiver := decodeTypedMessage[*proxyman.ReceiverConfig](t, config.ReceiverSettings)
	tlsConfig := decodeTypedMessage[*tlscfg.Config](t, receiver.GetStreamSettings().GetSecuritySettings()[0])
	if got, want := tlsConfig.GetNextProtocol(), []string{"h2", "http/1.1"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected tls alpn: got %v want %v", got, want)
	}
}

func decodeTypedMessage[T any](t *testing.T, message interface{ GetInstance() (proto.Message, error) }) T {
	t.Helper()

	instance, err := message.GetInstance()
	if err != nil {
		t.Fatalf("decode typed message failed: %v", err)
	}

	value, ok := instance.(T)
	if !ok {
		t.Fatalf("unexpected typed message type: got %T", instance)
	}
	return value
}

func writeTestCertificateFiles(t *testing.T) (string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key failed: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "node.example.com",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"node.example.com"},
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate failed: %v", err)
	}

	dir := t.TempDir()
	certFile := dir + "/test.crt"
	keyFile := dir + "/test.key"

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("write certificate failed: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("write private key failed: %v", err)
	}
	return certFile, keyFile
}
