package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/common/format"
	"github.com/keli-123456/kelinode/conf"
	kelicore "github.com/keli-123456/kelinode/core"
	"github.com/keli-123456/kelinode/limiter"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/tuic"
)

const (
	defaultHY2User      = "hy2-password"
	defaultTUICUUID     = "11111111-1111-1111-1111-111111111111"
	defaultTUICPassword = "tuic-password"
)

func main() {
	var (
		hy2Listen    = flag.String("hy2-listen", "127.0.0.1:29400", "HY2 listen address")
		tuicListen   = flag.String("tuic-listen", "127.0.0.1:29401", "TUIC listen address")
		certFile     = flag.String("cert", "", "TLS certificate file")
		keyFile      = flag.String("key", "", "TLS private key file")
		hy2User      = flag.String("hy2-user", defaultHY2User, "HY2 auth password")
		tuicUUID     = flag.String("tuic-uuid", defaultTUICUUID, "TUIC user UUID")
		tuicPassword = flag.String("tuic-password", defaultTUICPassword, "TUIC password")
		coreLogLevel = flag.String("core-log-level", "warning", "Xray core log level")
		accessLog    = flag.String("access-log", "none", "Xray access log path; use empty string for stdout")
	)
	flag.Parse()

	if *certFile == "" || *keyFile == "" {
		fail("both --cert and --key are required")
	}

	hy2Node, err := quicNode("hysteria2", "bench-go-hy2", *hy2Listen, *certFile, *keyFile)
	if err != nil {
		fail("build HY2 node: %v", err)
	}
	tuicNode, err := quicNode("tuic", "bench-go-tuic", *tuicListen, *certFile, *keyFile)
	if err != nil {
		fail("build TUIC node: %v", err)
	}

	cfg := conf.New()
	cfg.LogConfig.Level = *coreLogLevel
	cfg.LogConfig.CoreLevel = *coreLogLevel
	cfg.LogConfig.Access = *accessLog

	core := kelicore.New(cfg)
	if err := core.Start([]*panel.NodeInfo{hy2Node, tuicNode}); err != nil {
		fail("start old Go core: %v", err)
	}
	defer core.Close()

	if err := core.AddNode(hy2Node.Tag, hy2Node); err != nil {
		fail("add HY2 inbound: %v", err)
	}
	if err := core.AddNode(tuicNode.Tag, tuicNode); err != nil {
		fail("add TUIC inbound: %v", err)
	}

	limiter.Init()
	users := []panel.UserInfo{
		{Id: 1, Uuid: *hy2User},
		{Id: 2, Uuid: *tuicUUID},
	}
	limiter.AddLimiter(hy2Node.Type, hy2Node.Tag, users[:1], nil)
	limiter.AddLimiter(tuicNode.Type, tuicNode.Tag, users[1:], nil)
	defer limiter.DeleteLimiter(hy2Node.Tag)
	defer limiter.DeleteLimiter(tuicNode.Tag)

	if _, err := core.AddUsers(&kelicore.AddUsersParams{
		Tag:      hy2Node.Tag,
		NodeInfo: hy2Node,
		Users:    users[:1],
	}); err != nil {
		fail("add HY2 user: %v", err)
	}

	if err := addTUICUser(context.Background(), core, tuicNode.Tag, *tuicUUID, *tuicPassword); err != nil {
		fail("add TUIC user: %v", err)
	}

	fmt.Printf("old kelinode quic baseline ready\n")
	fmt.Printf("  hy2  %s user=%s\n", *hy2Listen, *hy2User)
	fmt.Printf("  tuic %s uuid=%s password=%s\n", *tuicListen, *tuicUUID, *tuicPassword)
	fmt.Printf("  cert %s\n", *certFile)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	fmt.Println("stopping old kelinode quic baseline")
}

func quicNode(protocolName, tag, listen, certFile, keyFile string) (*panel.NodeInfo, error) {
	host, portText, err := net.SplitHostPort(listen)
	if err != nil {
		return nil, fmt.Errorf("parse listen address %q: %w", listen, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, fmt.Errorf("parse listen port %q: %w", portText, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}

	common := &panel.CommonNode{
		Protocol:                protocolName,
		ListenIP:                host,
		ServerPort:              port,
		CongestionControl:       "bbr",
		ZeroRTTHandshake:        false,
		UpMbps:                  0,
		DownMbps:                0,
		Ignore_Client_Bandwidth: true,
		TlsSettings: panel.TlsSettings{
			ALPN: []string{"h3"},
		},
		CertInfo: &panel.CertInfo{
			CertMode: "file",
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}

	return &panel.NodeInfo{
		Id:           port,
		Type:         protocolName,
		Security:     panel.Tls,
		PushInterval: 60 * time.Second,
		PullInterval: 60 * time.Second,
		Tag:          tag,
		Common:       common,
	}, nil
}

func addTUICUser(ctx context.Context, core *kelicore.V2Core, tag, uuid, password string) error {
	manager, err := core.GetUserManager(tag)
	if err != nil {
		return fmt.Errorf("get TUIC user manager: %w", err)
	}
	user := &protocol.User{
		Level: 0,
		Email: format.UserTag(tag, uuid),
		Account: serial.ToTypedMessage(&tuic.Account{
			Uuid:     uuid,
			Password: password,
		}),
	}
	memoryUser, err := user.ToMemoryUser()
	if err != nil {
		return fmt.Errorf("build TUIC memory user: %w", err)
	}
	addCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := manager.AddUser(addCtx, memoryUser); err != nil {
		return fmt.Errorf("add TUIC user: %w", err)
	}
	return nil
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
