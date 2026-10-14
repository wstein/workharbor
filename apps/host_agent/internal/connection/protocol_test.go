// SPDX-License-Identifier: EUPL-1.2
package connection

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const owner = "11111111-1111-4111-8111-111111111111"
const host = "22222222-2222-4222-8222-222222222222"

func TestClosedAuthenticatedConnectionMessages(t *testing.T) {
	for _, kind := range []string{"hello", "hello_ack", "heartbeat", "heartbeat_ack"} {
		raw := `{"schema_version":1,"kind":"` + kind + `","owner_id":"` + owner + `","host_id":"` + host + `"`
		if kind == "hello" {
			raw += `,"capabilities":[]`
		}
		raw += `}`
		if _, err := DecodeMessage([]byte(raw), owner, host); err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeMessage([]byte(raw), host, owner); err == nil {
			t.Fatal("accepted scope")
		}
	}
}
func TestConnectionDecoderRejectsClosedShapeVersionTypesAndBytes(t *testing.T) {
	valid := `{"schema_version":1,"kind":"heartbeat","owner_id":"` + owner + `","host_id":"` + host + `"}`
	cases := []string{strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1), strings.Replace(valid, `"schema_version":1`, `"schema_version":1.0`, 1), strings.Replace(valid, `"kind":"heartbeat"`, `"kind":"heartbeat","kind":"heartbeat"`, 1), strings.TrimSuffix(valid, "}") + `,"token":"SECRET_SENTINEL"}`, valid + strings.Repeat(" ", 4096), strings.Replace(valid, "heartbeat", "command", 1)}
	for _, raw := range cases {
		_, err := DecodeMessage([]byte(raw), owner, host)
		if err == nil {
			t.Fatal("invalid accepted")
		}
		if strings.Contains(err.Error(), "SECRET_SENTINEL") {
			t.Fatal("leaked diagnostic")
		}
	}
}
func TestReconnectBackoffIsBoundedAndIndependentOfLifecycle(t *testing.T) {
	for i, n := range []int{1, 2, 4, 8, 16, 30, 30, 30} {
		if got := Backoff(i); got != n {
			t.Fatalf("attempt%d:%d", i, got)
		}
	}
}

func TestSharedHostConnectionFixtures(t *testing.T) {
	raw, e := os.ReadFile("../../../../specs/006-whr-cli/contracts/host-connection.fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		OwnerID string `json:"owner_id"`
		HostID  string `json:"host_id"`
		Cases   []struct {
			Name     string `json:"name"`
			Accepted bool   `json:"accepted"`
			Raw      string `json:"raw"`
		} `json:"cases"`
	}
	if e = json.Unmarshal(raw, &fixture); e != nil {
		t.Fatal(e)
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			_, e := DecodeMessage([]byte(c.Raw), fixture.OwnerID, fixture.HostID)
			if (e == nil) != c.Accepted {
				t.Fatal("shared fixture mismatch")
			}
		})
	}
}
