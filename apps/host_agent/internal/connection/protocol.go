// SPDX-License-Identifier: EUPL-1.2
package connection

import (
	"errors"
	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
	"math/big"
	"regexp"
)

var ErrProtocol = errors.New("invalid_connection_message")
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func DecodeMessage(raw []byte, owner, host string) (map[string]any, error) {
	if len(raw) > 4096 || !uuid.MatchString(owner) || !uuid.MatchString(host) {
		return nil, ErrProtocol
	}
	v, e := wirejson.Parse(raw)
	if e != nil {
		return nil, ErrProtocol
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrProtocol
	}
	version, ok := m["schema_version"].(*big.Int)
	if !ok || version.Cmp(big.NewInt(1)) != 0 || m["owner_id"] != owner || m["host_id"] != host {
		return nil, ErrProtocol
	}
	count := 4
	switch m["kind"] {
	case "hello":
		count = 5
		a, ok := m["capabilities"].([]any)
		if !ok || len(a) > 1 || (len(a) == 1 && a[0] != "docker") {
			return nil, ErrProtocol
		}
	case "hello_ack", "heartbeat", "heartbeat_ack":
	default:
		return nil, ErrProtocol
	}
	if len(m) != count {
		return nil, ErrProtocol
	}
	b, e := wirejson.CanonicalBytes(m)
	if e != nil || len(b) > 4096 {
		return nil, ErrProtocol
	}
	return m, nil
}
func Backoff(attempt int) int {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= 5 {
		return 30
	}
	return 1 << attempt
}
