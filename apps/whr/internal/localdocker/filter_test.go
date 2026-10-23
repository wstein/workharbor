// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"strings"
	"testing"
)

func TestLocalLsRoleAndLabelFilters(t *testing.T) {
	all := []Workspace{{Name: "primary", Role: "primary", Labels: map[string]string{"workharbor.workspace": "x", "project": "a=b"}}, {Name: "staging", Role: "staging", Labels: map[string]string{"anchorage-staging": "1", "project": ""}}, {Name: "temp", Role: "temp", Labels: map[string]string{"anchorage-temp": "1"}}, {Name: "foreign", Labels: map[string]string{"project": "a=b"}}}
	for _, tc := range []struct{ role, label, want string }{{"primary", "", "primary"}, {"staging", "", "staging"}, {"temp", "", "temp"}, {"", "project", "primary,staging"}, {"", "project=a=b", "primary"}, {"", "project=", "staging"}, {"primary", "project=a=b", "primary"}, {"", "absent", ""}, {"", "", "primary,staging,temp"}} {
		got, err := Filter(all, tc.role, tc.label)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, w := range got {
			names = append(names, w.Name)
		}
		if strings.Join(names, ",") != tc.want {
			t.Fatalf("%+v -> %v", tc, names)
		}
	}
	for _, tc := range []struct{ role, label string }{{"running", ""}, {"", "=x"}} {
		if _, err := Filter(all, tc.role, tc.label); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}
