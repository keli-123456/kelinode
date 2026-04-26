package conf

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const DefaultNodeConfigDir = "/etc/v2node"

type Conf struct {
	LogConfig      LogConfig      `mapstructure:"Log"`
	NodeConfigs    []NodeConfig   `mapstructure:"Nodes"`
	MachineConfig  MachineConfig  `mapstructure:"Machine"`
	PprofPort      int            `mapstructure:"PprofPort"`
	HealthPort     int            `mapstructure:"HealthPort"`
	RuntimeConfig  RuntimeConfig  `mapstructure:"Runtime"`
	RealtimeConfig RealtimeConfig `mapstructure:"Realtime"`
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
	Name      string `mapstructure:"Name"`
	APIHost   string `mapstructure:"ApiHost"`
	Key       string `mapstructure:"ApiKey"`
	MachineID int    `mapstructure:"MachineID"`
	Timeout   int    `mapstructure:"Timeout"`
	ConfigDir string `mapstructure:"ConfigDir"`
}

type RealtimeConfig struct {
	Enabled           bool   `mapstructure:"Enabled"`
	URL               string `mapstructure:"Url"`
	PingInterval      int    `mapstructure:"PingInterval"`
	ReconnectInterval int    `mapstructure:"ReconnectInterval"`
}

type RuntimeConfig struct {
	GoMemLimit string `mapstructure:"GoMemLimit"`
	GOGC       int    `mapstructure:"GOGC"`
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
			GoMemLimit: "",
			GOGC:       0,
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
		return nil
	}
	if err := v.Unmarshal(p); err != nil {
		return fmt.Errorf("unmarshal config error: %s", err)
	}
	normalizeNodeConfigs(p.NodeConfigs)
	normalizeMachineProfiles(p.MachineConfig.Profiles)
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

func NormalizeConfigDir(path string) string {
	if path == "" {
		return DefaultNodeConfigDir
	}
	return filepath.Clean(path)
}
