package conf

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Conf struct {
	LogConfig   LogConfig    `mapstructure:"Log"`
	NodeConfigs []NodeConfig `mapstructure:"Nodes"`
	PprofPort   int          `mapstructure:"PprofPort"`
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
	APIHost string `mapstructure:"ApiHost"`
	NodeID  int    `mapstructure:"NodeID"`
	Key     string `mapstructure:"ApiKey"`
	Timeout int    `mapstructure:"Timeout"`
}

func New() *Conf {
	return &Conf{
		LogConfig: LogConfig{
			Level:     "info",
			CoreLevel: "",
			Output:    "",
			Access:    "none",
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
	if err := v.Unmarshal(p); err != nil {
		return fmt.Errorf("unmarshal config error: %s", err)
	}
	return nil
}
