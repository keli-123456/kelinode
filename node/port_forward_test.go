package node

import (
	"reflect"
	"strconv"
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

func ruleTarget(port int) string {
	return "to=" + strconv.Itoa(port)
}
