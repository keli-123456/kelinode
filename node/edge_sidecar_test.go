package node

import (
	"strings"
	"testing"

	panel "github.com/keli-123456/kelinode/api/v2board"
)

func TestBuildMieruSidecarSpecUsesPanelUsersAndPortRange(t *testing.T) {
	t.Parallel()

	controller := &Controller{
		tag: "panel-a|mieru|7",
		info: &panel.NodeInfo{
			Id:   7,
			Type: "mieru",
			Common: &panel.CommonNode{
				Ports:     panel.PortValue("30000-30100"),
				Transport: "udp",
			},
		},
		userList: []panel.UserInfo{
			{Id: 1, Uuid: "user-a"},
			{Id: 2, Uuid: "user-b"},
		},
	}

	spec, err := controller.buildEdgeSidecarSpec()
	if err != nil {
		t.Fatalf("build edge sidecar spec failed: %v", err)
	}
	if spec.Protocol != "mieru" || !spec.Enabled || spec.Binary != "mita" {
		t.Fatalf("unexpected sidecar spec: %+v", spec)
	}
	if len(spec.GeneratedFiles) != 1 {
		t.Fatalf("unexpected generated files: %+v", spec.GeneratedFiles)
	}
	if got := spec.Env["MITA_CONFIG_JSON_FILE"]; got != spec.GeneratedFiles[0].Path {
		t.Fatalf("env config path mismatch: %q vs %q", got, spec.GeneratedFiles[0].Path)
	}
	contents := spec.GeneratedFiles[0].Contents
	for _, want := range []string{
		`"portRange":"30000-30100"`,
		`"protocol":"UDP"`,
		`"name":"user-a"`,
		`"password":"user-b"`,
	} {
		if !strings.Contains(contents, want) {
			t.Fatalf("generated config missing %s: %s", want, contents)
		}
	}
}

func TestBuildNaiveSidecarSpecUsesCaddyForwardProxy(t *testing.T) {
	t.Parallel()

	controller := &Controller{
		tag: "panel-a|naive|8",
		info: &panel.NodeInfo{
			Id:   8,
			Type: "naive",
			Common: &panel.CommonNode{
				ServerPort: 1443,
				TlsSettings: panel.TlsSettings{
					ServerName: "naive.example.com",
				},
				CertInfo: &panel.CertInfo{
					CertFile: "/etc/v2node/naive.crt",
					KeyFile:  "/etc/v2node/naive.key",
				},
			},
		},
		userList: []panel.UserInfo{
			{Id: 1, Uuid: "user-a"},
			{Id: 2, Uuid: "user-b"},
		},
	}

	spec, err := controller.buildEdgeSidecarSpec()
	if err != nil {
		t.Fatalf("build edge sidecar spec failed: %v", err)
	}
	if spec.Protocol != "naive" || !spec.Enabled || spec.Binary != "caddy" {
		t.Fatalf("unexpected sidecar spec: %+v", spec)
	}
	if got := strings.Join(spec.Args, " "); !strings.HasPrefix(got, "run --config runtime/naive-") {
		t.Fatalf("unexpected caddy args: %q", got)
	}
	if len(spec.GeneratedFiles) != 1 {
		t.Fatalf("unexpected generated files: %+v", spec.GeneratedFiles)
	}

	contents := spec.GeneratedFiles[0].Contents
	for _, want := range []string{
		":1443, naive.example.com {",
		"tls /etc/v2node/naive.crt /etc/v2node/naive.key",
		"forward_proxy {",
		"basic_auth user-a user-a",
		"basic_auth user-b user-b",
		"hide_ip",
		"hide_via",
	} {
		if !strings.Contains(contents, want) {
			t.Fatalf("generated caddyfile missing %s: %s", want, contents)
		}
	}
}
