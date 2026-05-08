package node

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	panel "github.com/keli-123456/kelinode/api/v2board"
	vCore "github.com/keli-123456/kelinode/core"
)

type EdgeSidecarSpec struct {
	Name           string
	Protocol       string
	Enabled        bool
	Binary         string
	Args           []string
	Env            map[string]string
	GeneratedFiles []EdgeGeneratedFile
}

type EdgeGeneratedFile struct {
	Path     string
	Contents string
}

type EdgeSidecarApplyReport struct {
	Started []string
	Stopped []string
	Failed  []EdgeSidecarFailure
}

type EdgeSidecarFailure struct {
	Name  string
	Error string
}

type EdgeSidecarBridge interface {
	UpsertSidecar(ctx context.Context, spec EdgeSidecarSpec) (EdgeSidecarApplyReport, error)
}

type mieruServerConfig struct {
	PortBindings []mieruPortBinding `json:"portBindings"`
	Users        []mieruUser        `json:"users"`
	LoggingLevel string             `json:"loggingLevel"`
	MTU          int                `json:"mtu"`
}

type mieruPortBinding struct {
	Port      int    `json:"port,omitempty"`
	PortRange string `json:"portRange,omitempty"`
	Protocol  string `json:"protocol"`
}

type mieruUser struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (c *Controller) applyEdgeSidecar(ctx context.Context) error {
	if c == nil || c.info == nil || !vCore.IsExternalSidecarNodeType(c.info.Type) {
		return nil
	}
	if c.edgeSidecarBridge == nil {
		return fmt.Errorf("keli-edge sidecar bridge is required for %s nodes", c.info.Type)
	}
	spec, err := c.buildEdgeSidecarSpec()
	if err != nil {
		return err
	}
	report, err := c.edgeSidecarBridge.UpsertSidecar(ctx, spec)
	if err != nil {
		return err
	}
	if len(report.Failed) > 0 {
		failure := report.Failed[0]
		return fmt.Errorf("keli-edge sidecar %s failed: %s", failure.Name, failure.Error)
	}
	return nil
}

func (c *Controller) buildEdgeSidecarSpec() (EdgeSidecarSpec, error) {
	if c == nil || c.info == nil {
		return EdgeSidecarSpec{}, fmt.Errorf("node info is empty")
	}
	switch c.info.Type {
	case "mieru":
		return c.buildMieruSidecarSpec()
	default:
		return EdgeSidecarSpec{}, fmt.Errorf("unsupported edge sidecar protocol: %s", c.info.Type)
	}
}

func (c *Controller) buildMieruSidecarSpec() (EdgeSidecarSpec, error) {
	if c.info.Common == nil {
		return EdgeSidecarSpec{}, fmt.Errorf("mieru node common config is empty")
	}
	if len(c.userList) == 0 {
		return EdgeSidecarSpec{}, fmt.Errorf("mieru node has no users")
	}

	name := edgeSidecarName("mieru", c.tag)
	configPath := "runtime/" + name + "/server.conf.json"
	config := mieruServerConfig{
		PortBindings: []mieruPortBinding{mieruPortBindingFromNode(c.info.Common)},
		Users:        mieruUsersFromPanel(c.userList),
		LoggingLevel: "INFO",
		MTU:          1400,
	}
	contents, err := json.Marshal(config)
	if err != nil {
		return EdgeSidecarSpec{}, fmt.Errorf("encode mieru sidecar config: %w", err)
	}

	return EdgeSidecarSpec{
		Name:     name,
		Protocol: "mieru",
		Enabled:  true,
		Binary:   "mita",
		Args:     []string{"run"},
		Env: map[string]string{
			"MITA_CONFIG_JSON_FILE": configPath,
		},
		GeneratedFiles: []EdgeGeneratedFile{{
			Path:     configPath,
			Contents: string(contents),
		}},
	}, nil
}

func mieruPortBindingFromNode(node *panel.CommonNode) mieruPortBinding {
	protocol := strings.ToUpper(strings.TrimSpace(node.Transport))
	if protocol == "" {
		protocol = "TCP"
	}
	if ports := strings.TrimSpace(node.Ports.String()); ports != "" {
		return mieruPortBinding{PortRange: ports, Protocol: protocol}
	}
	if port := strings.TrimSpace(node.Port.String()); port != "" && strings.Contains(port, "-") {
		return mieruPortBinding{PortRange: port, Protocol: protocol}
	}
	if node.ServerPort > 0 {
		return mieruPortBinding{Port: node.ServerPort, Protocol: protocol}
	}
	if port, err := strconv.Atoi(strings.TrimSpace(node.Port.String())); err == nil && port > 0 {
		return mieruPortBinding{Port: port, Protocol: protocol}
	}
	return mieruPortBinding{Protocol: protocol}
}

func mieruUsersFromPanel(users []panel.UserInfo) []mieruUser {
	result := make([]mieruUser, 0, len(users))
	for _, user := range users {
		if strings.TrimSpace(user.Uuid) == "" {
			continue
		}
		result = append(result, mieruUser{
			Name:     user.Uuid,
			Password: user.Uuid,
		})
	}
	return result
}

func edgeSidecarName(protocol string, tag string) string {
	sum := sha1.Sum([]byte(tag))
	return protocol + "-" + hex.EncodeToString(sum[:6])
}
