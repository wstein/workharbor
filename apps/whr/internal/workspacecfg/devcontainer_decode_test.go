// SPDX-License-Identifier: EUPL-1.2
package workspacecfg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeDevcontainerDataReusesStrictJSONC(t *testing.T) {
	data, err := DecodeDevcontainerData([]byte("{/*comment*/\"remoteUser\":\"vscode\",\"initializeCommand\":[\"sh\",\"init.sh\"],\"n\":1.0,}"))
	if err != nil {
		t.Fatal(err)
	}
	if data["remoteUser"] != "vscode" || data["n"] != json.Number("1.0") {
		t.Fatalf("data lost exact values: %#v", data)
	}
	for _, raw := range [][]byte{[]byte(`{"a":1,"a":2}`), []byte(`[]`), []byte("\xef\xbb\xbf{}"), []byte("{\x00}"), []byte("{\"x\":\"\xff\"}"), []byte(strings.Repeat("[", 130) + strings.Repeat("]", 130)), []byte(strings.Repeat(" ", MaxDevcontainerLen+1))} {
		if _, err = DecodeDevcontainerData(raw); err == nil {
			t.Fatalf("strict rejection lost for %q", raw[:min(len(raw), 40)])
		}
	}
}
