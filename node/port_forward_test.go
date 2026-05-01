package node

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	panel "github.com/keli-123456/kelinode/api/v2board"
)

func TestBuildHysteriaPortForwardRules(t *testing.T) {
	infos := []*panel.NodeInfo{
		{
			Id:   1,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 443,
				Port:       panel.PortValue("30000-30002"),
			},
		},
		{
			Id:   2,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 8443,
				Port:       panel.PortValue("20000,20001,20002"),
			},
		},
		{
			Id:   3,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 9443,
				Ports:      panel.PortValue("21000:21010"),
			},
		},
		{
			Id:   4,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 443,
				Port:       panel.PortValue("443"),
			},
		},
		{
			Id:   5,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 443,
				Port:       panel.PortValue("440-445"),
			},
		},
		{
			Id:   6,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 443,
				Port:       panel.PortValue("443,444,445"),
			},
		},
	}

	rules, errs := buildHysteriaPortForwardRules(infos)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	got := make([][]string, 0, len(rules))
	for _, rule := range rules {
		got = append(got, append(append([]string(nil), rule.matcher.args...), ruleTarget(rule.targetPort)))
	}
	want := [][]string{
		{"--dport", "30000:30002", "to=443"},
		{"-m", "multiport", "--dports", "20000,20001,20002", "to=8443"},
		{"--dport", "21000:21010", "to=9443"},
		{"--dport", "440:442", "to=443"},
		{"--dport", "444:445", "to=443"},
		{"-m", "multiport", "--dports", "444,445", "to=443"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected rules: got %#v want %#v", got, want)
	}
}

func TestParsePortForwardMatchersSplitsLargeMultiport(t *testing.T) {
	matchers, err := parsePortForwardMatchers("1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(matchers) != 2 {
		t.Fatalf("expected 2 matchers, got %d", len(matchers))
	}
	if got := matchers[0].args; !reflect.DeepEqual(got, []string{"-m", "multiport", "--dports", "1,2,3,4,5,6,7,8,9,10,11,12,13,14,15"}) {
		t.Fatalf("unexpected first matcher: %#v", got)
	}
	if got := matchers[1].args; !reflect.DeepEqual(got, []string{"--dport", "16"}) {
		t.Fatalf("unexpected second matcher: %#v", got)
	}
}

func TestParsePortForwardMatchersRejectsInvalidPorts(t *testing.T) {
	for _, input := range []string{"", "0", "65536", "300-200", "abc", "200,,201"} {
		if _, err := parsePortForwardMatchers(input); err == nil {
			t.Fatalf("expected %q to fail", input)
		}
	}
}

func TestBuildHysteriaPortForwardRulesRejectsOverlappingExternalPorts(t *testing.T) {
	infos := []*panel.NodeInfo{
		{
			Id:   1,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 443,
				Port:       panel.PortValue("30000-30002"),
			},
		},
		{
			Id:   2,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 8443,
				Port:       panel.PortValue("30002-30004"),
			},
		},
	}

	rules, errs := buildHysteriaPortForwardRules(infos)
	if len(errs) != 1 {
		t.Fatalf("expected one overlap error, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "overlaps node 1") {
		t.Fatalf("unexpected overlap error: %v", errs[0])
	}
	if len(rules) != 1 {
		t.Fatalf("expected only the non-conflicting rule, got %+v", rules)
	}
	got := append(append([]string(nil), rules[0].matcher.args...), ruleTarget(rules[0].targetPort))
	if !reflect.DeepEqual(got, []string{"--dport", "30000:30002", "to=443"}) {
		t.Fatalf("unexpected surviving rule: %#v", got)
	}
}

func TestBuildHysteriaPortForwardRulesRejectsExternalPortOverServerPort(t *testing.T) {
	infos := []*panel.NodeInfo{
		{
			Id:   1,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 443,
			},
		},
		{
			Id:   2,
			Type: "hysteria2",
			Common: &panel.CommonNode{
				ServerPort: 8443,
				Port:       panel.PortValue("440-445"),
			},
		},
	}

	rules, errs := buildHysteriaPortForwardRules(infos)
	if len(errs) != 1 {
		t.Fatalf("expected one server_port overlap error, got %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "server_port") {
		t.Fatalf("unexpected overlap error: %v", errs[0])
	}
	if len(rules) != 0 {
		t.Fatalf("expected conflicting redirect to be skipped, got %+v", rules)
	}
}

func TestReconcilePortForwardToolUsesDirectRedirectRules(t *testing.T) {
	oldRun := runPortForwardCommand
	oldOutput := runPortForwardCommandOutput
	defer func() {
		runPortForwardCommand = oldRun
		runPortForwardCommandOutput = oldOutput
	}()

	var commands [][]string
	runPortForwardCommand = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, append([]string{name}, args...))
		return nil
	}
	runPortForwardCommandOutput = func(_ context.Context, _ string, args ...string) (string, error) {
		if reflect.DeepEqual(args, []string{"-t", "nat", "-S", "PREROUTING"}) {
			return strings.Join([]string{
				"-A PREROUTING -p udp -j V2NODE-HY2",
				"-A PREROUTING -p udp --dport 10000:10002 -j V2NODE-HY2",
				"-A PREROUTING -p udp --dport 30000:30002 -m comment --comment \"V2NODE-HY2\" -j REDIRECT --to-ports 443",
				"-A PREROUTING -p tcp -j OTHER",
			}, "\n"), nil
		}
		return "", fmt.Errorf("unexpected output command: %v", args)
	}

	rules := []portForwardRule{
		{
			matcher:    portForwardMatcher{args: []string{"--dport", "30000:30002"}},
			targetPort: 443,
		},
		{
			matcher:    portForwardMatcher{args: []string{"-m", "multiport", "--dports", "20000,20001"}},
			targetPort: 8443,
		},
	}

	if err := reconcilePortForwardTool(context.Background(), "git", rules); err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	want := [][]string{
		{"git", "-t", "nat", "-D", "PREROUTING", "-p", "udp", "-j", "V2NODE-HY2"},
		{"git", "-t", "nat", "-D", "PREROUTING", "-p", "udp", "--dport", "10000:10002", "-j", "V2NODE-HY2"},
		{"git", "-t", "nat", "-D", "PREROUTING", "-p", "udp", "--dport", "30000:30002", "-m", "comment", "--comment", "V2NODE-HY2", "-j", "REDIRECT", "--to-ports", "443"},
		{"git", "-t", "nat", "-F", "V2NODE-HY2"},
		{"git", "-t", "nat", "-X", "V2NODE-HY2"},
		{"git", "-t", "nat", "-A", "PREROUTING", "-p", "udp", "--dport", "30000:30002", "-m", "comment", "--comment", "V2NODE-HY2", "-j", "REDIRECT", "--to-ports", "443"},
		{"git", "-t", "nat", "-A", "PREROUTING", "-p", "udp", "-m", "multiport", "--dports", "20000,20001", "-m", "comment", "--comment", "V2NODE-HY2", "-j", "REDIRECT", "--to-ports", "8443"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("unexpected commands:\ngot  %#v\nwant %#v", commands, want)
	}
}

func TestInspectPortForwardToolDetectsDrift(t *testing.T) {
	oldOutput := runPortForwardCommandOutput
	defer func() {
		runPortForwardCommandOutput = oldOutput
	}()

	runPortForwardCommandOutput = func(_ context.Context, _ string, args ...string) (string, error) {
		if reflect.DeepEqual(args, []string{"-t", "nat", "-S", "PREROUTING"}) {
			return strings.Join([]string{
				"-A PREROUTING -p udp -m udp --dport 10000:10002 -j V2NODE-HY2",
				"-A PREROUTING -p udp -m udp --dport 30000:30002 -m comment --comment \"V2NODE-HY2\" -j REDIRECT --to-ports 443",
			}, "\n"), nil
		}
		if reflect.DeepEqual(args, []string{"-t", "nat", "-S", "V2NODE-HY2"}) {
			return "-N V2NODE-HY2\n", nil
		}
		return "", fmt.Errorf("unexpected output command: %v", args)
	}

	rules := []portForwardRule{
		{
			matcher:    portForwardMatcher{args: []string{"--dport", "30000:30002"}},
			targetPort: 443,
		},
		{
			matcher:    portForwardMatcher{args: []string{"--dport", "20000:20002"}},
			targetPort: 8443,
		},
	}

	status := inspectPortForwardTool(context.Background(), "git", rules)
	if !status.Available {
		t.Fatalf("expected git to be available")
	}
	if !status.StaleChain {
		t.Fatalf("expected stale chain")
	}
	if len(status.Missing) != 1 || !strings.Contains(status.Missing[0], "20000:20002") {
		t.Fatalf("unexpected missing rules: %+v", status.Missing)
	}
	if len(status.Extra) != 1 || !strings.Contains(status.Extra[0], "V2NODE-HY2") {
		t.Fatalf("unexpected extra rules: %+v", status.Extra)
	}
}

func TestDisableAutoHY2PortForwardCleansExistingRulesOnce(t *testing.T) {
	oldCleanup := cleanupHysteriaPortForwardRuntime
	defer func() {
		cleanupHysteriaPortForwardRuntime = oldCleanup
	}()

	cleanupCalls := 0
	cleanupHysteriaPortForwardRuntime = func() {
		cleanupCalls++
	}

	n := &Node{autoHY2PortForward: true}
	n.SetAutoHY2PortForward(false)
	n.reconcileAutoHY2PortForward()
	n.reconcileAutoHY2PortForward()

	if cleanupCalls != 1 {
		t.Fatalf("expected cleanup to run once, got %d", cleanupCalls)
	}
}

func ruleTarget(port int) string {
	return "to=" + strconv.Itoa(port)
}
