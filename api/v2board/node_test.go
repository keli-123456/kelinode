package panel

import (
	"encoding/json"
	"testing"
)

func TestCommonNodePortAcceptsStringAndNumber(t *testing.T) {
	for _, body := range []string{
		`{"port":"30000-30100","ports":"40000-40100"}`,
		`{"port":443,"ports":8443}`,
	} {
		var node CommonNode
		if err := json.Unmarshal([]byte(body), &node); err != nil {
			t.Fatalf("unmarshal %s failed: %v", body, err)
		}
		if node.Port.String() == "" || node.Ports.String() == "" {
			t.Fatalf("expected port fields from %s, got port=%q ports=%q", body, node.Port, node.Ports)
		}
	}
}
