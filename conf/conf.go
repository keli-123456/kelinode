package conf

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const DefaultNodeConfigDir = "/etc/v2node"
const DefaultEdgeURL = "http://127.0.0.1:17990"

type Conf struct {
	LogConfig      LogConfig      `mapstructure:"Log"`
	NodeConfigs    []NodeConfig   `mapstructure:"Nodes"`
	MachineConfig  MachineConfig  `mapstructure:"Machine"`
	AgentConfig    AgentConfig    `mapstructure:"Agent"`
	DNSConfig      DNSConfig      `mapstructure:"DNS"`
	PprofPort      int            `mapstructure:"PprofPort"`
	HealthPort     int            `mapstructure:"HealthPort"`
	RuntimeConfig  RuntimeConfig  `mapstructure:"Runtime"`
	RealtimeConfig RealtimeConfig `mapstructure:"Realtime"`
	EdgeConfig     EdgeConfig     `mapstructure:"Edge"`
}

type LogConfig struct {
	// Level controls v2node's own logrus level.
	Level string `mapstructure:"Level"`
	// CoreLevel controls Xray Core's log level. If empty, it falls back to Level.
	CoreLevel string `mapstructure:"CoreLevel"`
	// Output is v2node's log output file path. If empty, logs go to stdout.
	// Note: currently Xray Core's error log path also uses this value.
	Output string `mapstructure:"Output"`
	// Access controls Xray Core access log output ("none" disables access logs).
	Access string `mapstructure:"Access"`
}

type NodeConfig struct {
	APIHost   string `mapstructure:"ApiHost"`
	NodeID    int    `mapstructure:"NodeID"`
	Key       string `mapstructure:"ApiKey"`
	MachineID int    `mapstructure:"MachineID"`
	Timeout   int    `mapstructure:"Timeout"`
	ConfigDir string `mapstructure:"ConfigDir"`
}

type MachineConfig struct {
	Enabled         bool                   `mapstructure:"Enabled"`
	ContinueOnError bool                   `mapstructure:"ContinueOnError"`
	Profiles        []MachineProfileConfig `mapstructure:"Profiles"`
}

type MachineProfileConfig struct {
	Name      string         `mapstructure:"Name"`
	APIHost   string         `mapstructure:"ApiHost"`
	Key       string         `mapstructure:"ApiKey"`
	MachineID int            `mapstructure:"MachineID"`
	Timeout   int            `mapstructure:"Timeout"`
	ConfigDir string         `mapstructure:"ConfigDir"`
	Realtime  RealtimeConfig `mapstructure:"-"`
}

type RealtimeConfig struct {
	Enabled           bool   `mapstructure:"Enabled"`
	URL               string `mapstructure:"Url"`
	PingInterval      int    `mapstructure:"PingInterval"`
	ReconnectInterval int    `mapstructure:"ReconnectInterval"`
}

type AgentConfig struct {
	SubscriptionProxy SubscriptionProxyConfig `mapstructure:"SubscriptionProxy" json:"subscription_proxy"`
}

type SubscriptionProxyConfig struct {
	Enabled           bool                       `mapstructure:"Enabled" json:"enabled"`
	HTTPSListen       string                     `mapstructure:"HTTPSListen" json:"https_listen"`
	HTTPListen        string                     `mapstructure:"HTTPListen" json:"http_listen"`
	CertFile          string                     `mapstructure:"CertFile" json:"cert_file"`
	KeyFile           string                     `mapstructure:"KeyFile" json:"key_file"`
	CertificateDomain string                     `mapstructure:"CertificateDomain" json:"certificate_domain"`
	ChallengeDir      string                     `mapstructure:"ChallengeDir" json:"challenge_dir"`
	ZeroSSL           ZeroSSLConfig              `mapstructure:"ZeroSSL" json:"zerossl"`
	SiteID            string                     `mapstructure:"SiteID" json:"site_id"`
	UpstreamBaseURL   string                     `mapstructure:"UpstreamBaseURL" json:"upstream_base_url"`
	SubscribePath     string                     `mapstructure:"SubscribePath" json:"subscribe_path"`
	AllowHTTPFallback bool                       `mapstructure:"AllowHTTPFallback" json:"allow_http_fallback"`
	MaxResponseBytes  int64                      `mapstructure:"MaxResponseBytes" json:"max_response_bytes"`
	Profiles          []SubscriptionProxyProfile `mapstructure:"Profiles" json:"profiles"`
}

type SubscriptionProxyProfile struct {
	SiteID          string `mapstructure:"SiteID" json:"site_id"`
	UpstreamBaseURL string `mapstructure:"UpstreamBaseURL" json:"upstream_base_url"`
	SubscribePath   string `mapstructure:"SubscribePath" json:"subscribe_path"`
}

type ZeroSSLConfig struct {
	Status            string `mapstructure:"Status" json:"status"`
	CertificateID     string `mapstructure:"CertificateID" json:"certificate_id"`
	ValidationPath    string `mapstructure:"ValidationPath" json:"validation_path"`
	ValidationContent any    `mapstructure:"ValidationContent" json:"validation_content"`
	CertificatePEM    string `mapstructure:"CertificatePEM" json:"certificate_pem"`
	CABundlePEM       string `mapstructure:"CABundlePEM" json:"ca_bundle_pem"`
	ExpiresAt         string `mapstructure:"ExpiresAt" json:"expires_at"`
}

type RuntimeConfig struct {
	GoMemLimit         string `mapstructure:"GoMemLimit"`
	GOGC               int    `mapstructure:"GOGC"`
	AutoHY2PortForward bool   `mapstructure:"AutoHY2PortForward"`
}

type EdgeConfig struct {
	Enabled bool   `mapstructure:"Enabled"`
	URL     string `mapstructure:"URL"`
	Timeout int    `mapstructure:"Timeout"`
}

type DNSConfig struct {
	Servers       []string `mapstructure:"Servers"`
	QueryStrategy string   `mapstructure:"QueryStrategy"`
}

func New() *Conf {
	return &Conf{
		LogConfig: LogConfig{
			Level:     "info",
			CoreLevel: "",
			Output:    "",
			Access:    "none",
		},
		HealthPort: 0,
		RuntimeConfig: RuntimeConfig{
			GoMemLimit:         "",
			GOGC:               0,
			AutoHY2PortForward: false,
		},
		EdgeConfig: EdgeConfig{
			Enabled: false,
			URL:     DefaultEdgeURL,
			Timeout: 2,
		},
	}
}

func (p *Conf) LoadFromPath(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open config file error: %s", err)
	}
	defer f.Close()
	v := viper.New()
	v.SetConfigFile(filePath)
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("read config file error: %s", err)
	}
	if isConfigV2(v) {
		if err := p.loadFromV2(v); err != nil {
			return err
		}
		normalizeNodeConfigs(p.NodeConfigs)
		normalizeMachineProfiles(p.MachineConfig.Profiles)
		normalizeEdgeConfig(&p.EdgeConfig)
		return nil
	}
	if err := v.Unmarshal(p); err != nil {
		return fmt.Errorf("unmarshal config error: %s", err)
	}
	normalizeNodeConfigs(p.NodeConfigs)
	normalizeMachineProfiles(p.MachineConfig.Profiles)
	normalizeEdgeConfig(&p.EdgeConfig)
	return nil
}

func ResolveConfigPath(filePath string) string {
	if filePath == "" {
		return filePath
	}
	if _, err := os.Stat(filePath); err == nil {
		return filePath
	}

	ext := filepath.Ext(filePath)
	base := filePath[:len(filePath)-len(ext)]
	switch ext {
	case ".json":
		for _, candidate := range []string{base + ".yml", base + ".yaml"} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	case ".yml", ".yaml":
		candidate := base + ".json"
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filePath
}

func normalizeNodeConfigs(nodes []NodeConfig) {
	for i := range nodes {
		nodes[i].ConfigDir = NormalizeConfigDir(nodes[i].ConfigDir)
	}
}

func normalizeMachineProfiles(profiles []MachineProfileConfig) {
	for i := range profiles {
		if profiles[i].ConfigDir != "" {
			profiles[i].ConfigDir = NormalizeConfigDir(profiles[i].ConfigDir)
		}
	}
}

func normalizeEdgeConfig(edge *EdgeConfig) {
	if edge == nil {
		return
	}
	if edge.URL == "" {
		edge.URL = DefaultEdgeURL
	}
	if edge.Timeout <= 0 {
		edge.Timeout = 2
	}
}

func NormalizeConfigDir(path string) string {
	if path == "" {
		return DefaultNodeConfigDir
	}
	return filepath.Clean(path)
}
