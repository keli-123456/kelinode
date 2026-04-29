package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/keli-123456/kelinode/conf"
	"github.com/keli-123456/kelinode/node"
	"github.com/spf13/cobra"
)

var (
	rulesConfig string
	rulesJSON   bool
)

var rulesCommand = cobra.Command{
	Use:   "rules",
	Short: "Inspect or repair v2node managed system rules",
}

var rulesStatusCommand = cobra.Command{
	Use:   "status",
	Short: "Show HY2 port forwarding rule status",
	RunE: func(_ *cobra.Command, _ []string) error {
		status, failures, err := loadRulesStatus(false)
		if err != nil {
			return err
		}
		renderRulesStatus(status, failures)
		return nil
	},
}

var rulesRepairCommand = cobra.Command{
	Use:   "repair",
	Short: "Apply expected HY2 port forwarding rules",
	RunE: func(_ *cobra.Command, _ []string) error {
		status, failures, err := loadRulesStatus(true)
		if err != nil {
			return err
		}
		renderRulesStatus(status, failures)
		if !status.RunningAsRoot && (len(status.ExpectedRules) > 0 || node.HysteriaPortForwardNeedsRepair(status)) {
			return fmt.Errorf("repair requires root")
		}
		return nil
	},
}

var rulesCleanupCommand = cobra.Command{
	Use:   "cleanup",
	Short: "Remove v2node managed HY2 port forwarding rules",
	RunE: func(_ *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		status := node.CleanupHysteriaPortForward(ctx)
		renderRulesStatus(status, nil)
		if !status.RunningAsRoot {
			return fmt.Errorf("cleanup requires root")
		}
		return nil
	},
}

func init() {
	rulesCommand.PersistentFlags().
		StringVarP(&rulesConfig, "config", "c", "/etc/v2node/config.json", "config file path")
	rulesCommand.PersistentFlags().
		BoolVar(&rulesJSON, "json", false, "print JSON output")
	rulesCommand.AddCommand(&rulesStatusCommand, &rulesRepairCommand, &rulesCleanupCommand)
	command.AddCommand(&rulesCommand)
}

func loadRulesStatus(repair bool) (node.HysteriaPortForwardStatus, []node.NodeFailure, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := conf.New()
	configPath := conf.ResolveConfigPath(rulesConfig)
	if err := cfg.LoadFromPath(configPath); err != nil {
		return node.HysteriaPortForwardStatus{}, nil, err
	}
	if err := node.ResolveMachineNodeConfigs(ctx, cfg); err != nil {
		return node.HysteriaPortForwardStatus{}, nil, err
	}

	infos, failures, err := node.FetchNodeInfos(ctx, cfg.NodeConfigs, node.MachineOptions{
		ContinueOnError: cfg.MachineConfig.ContinueOnError,
	})
	if err != nil {
		return node.HysteriaPortForwardStatus{}, failures, err
	}

	if repair {
		status := node.RepairHysteriaPortForward(ctx, infos)
		status.Enabled = cfg.RuntimeConfig.AutoHY2PortForward
		return status, failures, nil
	}

	status := node.InspectHysteriaPortForward(ctx, infos)
	status.Enabled = cfg.RuntimeConfig.AutoHY2PortForward
	return status, failures, nil
}

func renderRulesStatus(status node.HysteriaPortForwardStatus, failures []node.NodeFailure) {
	if rulesJSON {
		payload := struct {
			Status       node.HysteriaPortForwardStatus `json:"status"`
			NodeFailures []map[string]any               `json:"node_failures,omitempty"`
		}{
			Status:       status,
			NodeFailures: formatRuleNodeFailures(failures),
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("auto_hy2_port_forward: %t\n", status.Enabled)
	fmt.Printf("running_as_root: %t\n", status.RunningAsRoot)
	fmt.Printf("expected_rules: %d\n", len(status.ExpectedRules))
	for _, rule := range status.ExpectedRules {
		fmt.Printf("  - udp %s -> %d\n", rule.Match, rule.TargetPort)
	}
	if len(failures) > 0 {
		fmt.Printf("node_failures: %d\n", len(failures))
		for _, failure := range failures {
			message := ""
			if failure.Err != nil {
				message = failure.Err.Error()
			}
			fmt.Printf("  - %s node=%d machine=%d error=%s\n",
				strings.TrimRight(failure.Config.APIHost, "/"),
				failure.Config.NodeID,
				failure.Config.MachineID,
				message,
			)
		}
	}

	for _, tool := range status.Tools {
		fmt.Printf("%s: %s\n", tool.Tool, renderToolState(tool))
		if tool.StaleChain {
			fmt.Printf("  stale_chain: %s\n", "yes")
		}
		if len(tool.Missing) > 0 {
			fmt.Printf("  missing: %d\n", len(tool.Missing))
			for _, spec := range tool.Missing {
				fmt.Printf("    - %s\n", spec)
			}
		}
		if len(tool.Extra) > 0 {
			fmt.Printf("  extra: %d\n", len(tool.Extra))
			for _, spec := range tool.Extra {
				fmt.Printf("    - %s\n", spec)
			}
		}
		if tool.Error != "" {
			fmt.Printf("  error: %s\n", tool.Error)
		}
	}
	if len(status.Errors) > 0 {
		fmt.Printf("errors: %d\n", len(status.Errors))
		for _, err := range status.Errors {
			fmt.Printf("  - %s\n", err)
		}
	}
}

func renderToolState(tool node.HysteriaPortForwardToolStatus) string {
	if !tool.Available {
		return "unavailable"
	}
	if tool.Error != "" {
		return "error"
	}
	if len(tool.Missing) > 0 || len(tool.Extra) > 0 || tool.StaleChain {
		return "needs-repair"
	}
	return "ok"
}

func formatRuleNodeFailures(failures []node.NodeFailure) []map[string]any {
	if len(failures) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(failures))
	for _, failure := range failures {
		item := map[string]any{
			"api_host":   failure.Config.APIHost,
			"node_id":    failure.Config.NodeID,
			"machine_id": failure.Config.MachineID,
		}
		if failure.Err != nil {
			item["error"] = failure.Err.Error()
		}
		out = append(out, item)
	}
	return out
}
