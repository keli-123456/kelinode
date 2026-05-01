package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	version  = "TempVersion" //use ldflags replace
	codename = "v2node"
	intro    = "A V2board backend based on modified xray-core"
)

var installedVersionPaths = []string{
	"/usr/local/v2node/.installed_version",
}

var versionCommand = cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(_ *cobra.Command, _ []string) {
		showVersion()
	},
}

func init() {
	command.AddCommand(&versionCommand)
}

func showVersion() {
	displayVersion := currentKelinodeVersion()
	if displayVersion == "" {
		displayVersion = "unknown"
	}
	fmt.Printf("%s %s (%s) \n", codename, displayVersion, intro)
}

func currentKelinodeVersion() string {
	if v := strings.TrimSpace(version); v != "" && !isPlaceholderVersion(v) {
		return v
	}

	for _, path := range installedVersionPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if v := strings.TrimSpace(string(data)); v != "" {
			return v
		}
	}

	return ""
}

func isPlaceholderVersion(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "TempVersion" || value == "dev"
}
