package conf

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type configV2 struct {
	Panel    panelConfigV2    `mapstructure:"panel"`
	Kernel   kernelConfigV2   `mapstructure:"kernel"`
	Log      logConfigV2      `mapstructure:"log"`
	Runtime  runtimeConfigV2  `mapstructure:"runtime"`
	Realtime realtimeConfigV2 `mapstructure:"realtime"`
	Machine  machineConfigV2  `mapstructure:"machine"`
	Agent    agentConfigV2    `mapstructure:"agent"`
	Health   int              `mapstructure:"health_port"`
	Pprof    int              `mapstructure:"pprof_port"`
	Nodes    []nodeConfigV2   `mapstructure:"nodes"`
}

type panelConfigV2 struct {
	URL       string `mapstructure:"url"`
	Token     string `mapstructure:"token"`
	NodeID    int    `mapstructure:"node_id"`
	MachineID int    `mapstructure:"machine_id"`
	Timeout   int    `mapstructure:"timeout"`
}

type kernelConfigV2 struct {
	Type      string `mapstructure:"type"`
	ConfigDir string `mapstructure:"config_dir"`
	LogLevel  string `mapstructure:"log_level"`
}

type logConfigV2 struct {
	Level  string `mapstructure:"level"`
	Output string `mapstructure:"output"`
	Access string `mapstructure:"access"`
}

type runtimeConfigV2 struct {
	GoMemLimit         string `mapstructure:"gomemlimit"`
	GOGC               int    `mapstructure:"gogc"`
	AutoHY2PortForward bool   `mapstructure:"auto_hy2_port_forward"`
}

type realtimeConfigV2 struct {
	Enabled           *bool  `mapstructure:"enabled"`
	URL               string `mapstructure:"url"`
	PingInterval      int    `mapstructure:"ping_interval"`
	ReconnectInterval int    `mapstructure:"reconnect_interval"`
}

type machineConfigV2 struct {
	Enabled         bool                     `mapstructure:"enabled"`
	ContinueOnError *bool                    `mapstructure:"continue_on_error"`
	Profiles        []machineProfileConfigV2 `mapstructure:"profiles"`
}

type machineProfileConfigV2 struct {
	Name      string `mapstructure:"name"`
	URL       string `mapstructure:"url"`
	Token     string `mapstructure:"token"`
	MachineID int    `mapstructure:"machine_id"`
	Timeout   int    `mapstructure:"timeout"`
	ConfigDir string `mapstructure:"config_dir"`
}

type agentConfigV2 struct {
	SubscriptionProxy subscriptionProxyConfigV2 `mapstructure:"subscription_proxy"`
}

type subscriptionProxyConfigV2 struct {
	Enabled           bool                         `mapstructure:"enabled"`
	HTTPSListen       string                       `mapstructure:"https_listen"`
	HTTPListen        string                       `mapstructure:"http_listen"`
	CertFile          string                       `mapstructure:"cert_file"`
	KeyFile           string                       `mapstructure:"key_file"`
	CertificateDomain string                       `mapstructure:"certificate_domain"`
	ChallengeDir      string                       `mapstructure:"challenge_dir"`
	ZeroSSL           zeroSSLConfigV2              `mapstructure:"zerossl"`
	SiteID            string                       `mapstructure:"site_id"`
	UpstreamBaseURL   string                       `mapstructure:"upstream_base_url"`
	SubscribePath     string                       `mapstructure:"subscribe_path"`
	AllowHTTPFallback bool                         `mapstructure:"allow_http_fallback"`
	MaxResponseBytes  int64                        `mapstructure:"max_response_bytes"`
	Profiles          []subscriptionProxyProfileV2 `mapstructure:"profiles"`
}

type subscriptionProxyProfileV2 struct {
	SiteID          string `mapstructure:"site_id"`
	UpstreamBaseURL string `mapstructure:"upstream_base_url"`
	SubscribePath   string `mapstructure:"subscribe_path"`
}

type zeroSSLConfigV2 struct {
	Status            string `mapstructure:"status"`
	CertificateID     string `mapstructure:"certificate_id"`
	ValidationPath    string `mapstructure:"validation_path"`
	ValidationContent any    `mapstructure:"validation_content"`
	CertificatePEM    string `mapstructure:"certificate_pem"`
	CABundlePEM       string `mapstructure:"ca_bundle_pem"`
	ExpiresAt         string `mapstructure:"expires_at"`
}

type nodeConfigV2 struct {
	URL       string `mapstructure:"url"`
	Token     string `mapstructure:"token"`
	NodeID    int    `mapstructure:"node_id"`
	MachineID int    `mapstructure:"machine_id"`
	Timeout   int    `mapstructure:"timeout"`
	ConfigDir string `mapstructure:"config_dir"`
}

func isConfigV2(v *viper.Viper) bool {
	if v == nil {
		return false
	}
	return v.IsSet("panel") || v.IsSet("kernel") || v.IsSet("runtime") || v.IsSet("health_port") || v.IsSet("machine")
}

func (p *Conf) loadFromV2(v *viper.Viper) error {
	cfg := configV2{}
	if err := v.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("unmarshal config v2 error: %w", err)
	}

	if strings.TrimSpace(cfg.Log.Level) != "" {
		p.LogConfig.Level = strings.TrimSpace(cfg.Log.Level)
	}
	if strings.TrimSpace(cfg.Log.Output) != "" {
		p.LogConfig.Output = strings.TrimSpace(cfg.Log.Output)
	}
	if strings.TrimSpace(cfg.Log.Access) != "" {
		p.LogConfig.Access = strings.TrimSpace(cfg.Log.Access)
	}
	if strings.TrimSpace(cfg.Kernel.LogLevel) != "" {
		p.LogConfig.CoreLevel = strings.TrimSpace(cfg.Kernel.LogLevel)
	}

	p.HealthPort = cfg.Health
	p.PprofPort = cfg.Pprof
	p.RuntimeConfig = RuntimeConfig{
		GoMemLimit:         strings.TrimSpace(cfg.Runtime.GoMemLimit),
		GOGC:               cfg.Runtime.GOGC,
		AutoHY2PortForward: cfg.Runtime.AutoHY2PortForward,
	}
	if cfg.Realtime.Enabled != nil {
		p.RealtimeConfig.Enabled = *cfg.Realtime.Enabled
	}
	if strings.TrimSpace(cfg.Realtime.URL) != "" {
		p.RealtimeConfig.URL = strings.TrimSpace(cfg.Realtime.URL)
	}
	if cfg.Realtime.PingInterval > 0 {
		p.RealtimeConfig.PingInterval = cfg.Realtime.PingInterval
	}
	if cfg.Realtime.ReconnectInterval > 0 {
		p.RealtimeConfig.ReconnectInterval = cfg.Realtime.ReconnectInterval
	}
	p.MachineConfig.Enabled = cfg.Machine.Enabled || len(cfg.Machine.Profiles) > 0
	if cfg.Machine.ContinueOnError != nil {
		p.MachineConfig.ContinueOnError = *cfg.Machine.ContinueOnError
	} else if p.MachineConfig.Enabled {
		p.MachineConfig.ContinueOnError = true
	}

	baseAPIHost := strings.TrimSpace(cfg.Panel.URL)
	baseToken := strings.TrimSpace(cfg.Panel.Token)
	baseTimeout := cfg.Panel.Timeout
	baseConfigDir := NormalizeConfigDir(cfg.Kernel.ConfigDir)
	machineProfiles := make([]MachineProfileConfig, 0, len(cfg.Machine.Profiles))
	for _, row := range cfg.Machine.Profiles {
		apiHost := firstNonEmpty(strings.TrimSpace(row.URL), baseAPIHost)
		token := firstNonEmpty(strings.TrimSpace(row.Token), baseToken)
		timeout := row.Timeout
		if timeout <= 0 {
			timeout = baseTimeout
		}
		name := strings.TrimSpace(row.Name)
		if name == "" && row.MachineID > 0 {
			name = fmt.Sprintf("machine-%d", row.MachineID)
		}
		if apiHost == "" || token == "" || row.MachineID <= 0 {
			return fmt.Errorf("config v2 machine profiles require url, token and machine_id")
		}
		machineProfiles = append(machineProfiles, MachineProfileConfig{
			Name:      name,
			APIHost:   apiHost,
			Key:       token,
			MachineID: row.MachineID,
			Timeout:   timeout,
			ConfigDir: strings.TrimSpace(row.ConfigDir),
		})
	}
	p.MachineConfig.Profiles = machineProfiles
	p.AgentConfig.SubscriptionProxy = subscriptionProxyFromV2(cfg.Agent.SubscriptionProxy)

	if len(cfg.Nodes) == 0 {
		if len(machineProfiles) > 0 {
			p.NodeConfigs = nil
			return nil
		}
		if baseAPIHost == "" || baseToken == "" || cfg.Panel.NodeID <= 0 {
			return fmt.Errorf("config v2 requires panel.url, panel.token and panel.node_id when nodes is empty")
		}
		p.NodeConfigs = []NodeConfig{
			{
				APIHost:   baseAPIHost,
				NodeID:    cfg.Panel.NodeID,
				Key:       baseToken,
				MachineID: cfg.Panel.MachineID,
				Timeout:   baseTimeout,
				ConfigDir: baseConfigDir,
			},
		}
		return nil
	}

	multiNode := len(cfg.Nodes) > 1
	nodes := make([]NodeConfig, 0, len(cfg.Nodes))
	for _, row := range cfg.Nodes {
		apiHost := firstNonEmpty(strings.TrimSpace(row.URL), baseAPIHost)
		token := firstNonEmpty(strings.TrimSpace(row.Token), baseToken)
		timeout := row.Timeout
		if timeout <= 0 {
			timeout = baseTimeout
		}
		if apiHost == "" || token == "" || row.NodeID <= 0 {
			return fmt.Errorf("config v2 nodes entries require node_id and inherit or define url/token")
		}
		nodes = append(nodes, NodeConfig{
			APIHost:   apiHost,
			NodeID:    row.NodeID,
			Key:       token,
			MachineID: firstPositive(row.MachineID, cfg.Panel.MachineID),
			Timeout:   timeout,
			ConfigDir: resolveNodeConfigDir(baseConfigDir, row.ConfigDir, row.NodeID, multiNode),
		})
	}
	p.NodeConfigs = nodes
	return nil
}

func subscriptionProxyFromV2(src subscriptionProxyConfigV2) SubscriptionProxyConfig {
	profiles := make([]SubscriptionProxyProfile, 0, len(src.Profiles))
	for _, row := range src.Profiles {
		profiles = append(profiles, SubscriptionProxyProfile{
			SiteID:          strings.TrimSpace(row.SiteID),
			UpstreamBaseURL: strings.TrimSpace(row.UpstreamBaseURL),
			SubscribePath:   strings.Trim(strings.TrimSpace(row.SubscribePath), "/"),
		})
	}
	return SubscriptionProxyConfig{
		Enabled:           src.Enabled || len(profiles) > 0,
		HTTPSListen:       strings.TrimSpace(src.HTTPSListen),
		HTTPListen:        strings.TrimSpace(src.HTTPListen),
		CertFile:          strings.TrimSpace(src.CertFile),
		KeyFile:           strings.TrimSpace(src.KeyFile),
		CertificateDomain: strings.TrimSpace(src.CertificateDomain),
		ChallengeDir:      strings.TrimSpace(src.ChallengeDir),
		ZeroSSL:           confZeroSSLFromV2(src.ZeroSSL),
		SiteID:            strings.TrimSpace(src.SiteID),
		UpstreamBaseURL:   strings.TrimRight(strings.TrimSpace(src.UpstreamBaseURL), "/"),
		SubscribePath:     strings.Trim(strings.TrimSpace(src.SubscribePath), "/"),
		AllowHTTPFallback: src.AllowHTTPFallback,
		MaxResponseBytes:  src.MaxResponseBytes,
		Profiles:          profiles,
	}
}

func confZeroSSLFromV2(src zeroSSLConfigV2) ZeroSSLConfig {
	return ZeroSSLConfig{
		Status:            strings.TrimSpace(src.Status),
		CertificateID:     strings.TrimSpace(src.CertificateID),
		ValidationPath:    strings.TrimSpace(src.ValidationPath),
		ValidationContent: src.ValidationContent,
		CertificatePEM:    strings.TrimSpace(src.CertificatePEM),
		CABundlePEM:       strings.TrimSpace(src.CABundlePEM),
		ExpiresAt:         strings.TrimSpace(src.ExpiresAt),
	}
}

func resolveNodeConfigDir(baseDir string, override string, nodeID int, multiNode bool) string {
	if strings.TrimSpace(override) != "" {
		return NormalizeConfigDir(override)
	}
	root := NormalizeConfigDir(baseDir)
	if multiNode {
		return filepath.Join(root, fmt.Sprintf("node-%d", nodeID))
	}
	return root
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
