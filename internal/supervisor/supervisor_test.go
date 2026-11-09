package supervisor

import (
	"testing"

	"github.com/zeroflow/esphome-sil/internal/apiclient"
	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/instance"
)

// The readiness identity check: only the instance's own firmware (device
// name and MAC, case and colon insensitive) counts as ready.
func TestMismatch(t *testing.T) {
	id := instance.Identity{Name: "a", APIPort: 6054}
	mac := instance.MAC("a")
	own := apiclient.Hello{ServerName: "sil-a", Name: "sil-a", MAC: "02" + mac[3:5] + mac[6:8] + mac[9:11] + mac[12:14] + mac[15:17]}
	if bad := mismatch(own, id); bad != nil {
		t.Fatalf("own firmware rejected: %v", bad)
	}
	upper := own
	upper.MAC = "02:" + mac[3:]
	if bad := mismatch(apiclient.Hello{ServerName: "sil-a", Name: "sil-a", MAC: upper.MAC}, id); bad != nil {
		t.Errorf("colon form rejected: %v", bad)
	}

	for name, h := range map[string]apiclient.Hello{
		"other instance": {ServerName: "sil-b", Name: "sil-b", MAC: own.MAC},
		"other mac":      {ServerName: "sil-a", Name: "sil-a", MAC: "02aaaaaaaa01"},
		"real device":    {ServerName: "fancontroller", Name: "fancontroller", MAC: "a0b1c2d3e4f5"},
		"response name":  {ServerName: "sil-a", Name: "sil-b", MAC: own.MAC},
	} {
		bad := mismatch(h, id)
		if bad == nil || bad.Code != "api_identity_mismatch" || bad.Exit != apperr.ExitInfra {
			t.Errorf("%s: %v, want api_identity_mismatch (exit 2)", name, bad)
		}
	}
}
