// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"reflect"
	"testing"
)

func TestInitOverwriteFlagsRetainPaths(t *testing.T) {
	for _, spelling := range [][]string{{"workspace", "init"}, {"ws", "init"}, {"init"}} {
		a := &app{colorFlag: "auto"}
		a.build()
		c, _, err := a.root.Find(spelling)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ParseFlags([]string{"--overwrite=workharbor.json", "--overwrite", "projects/a,b.json"}); err != nil {
			t.Fatalf("%v: %v", spelling, err)
		}
		got, err := c.Flags().GetStringArray("overwrite")
		if err != nil {
			t.Fatalf("%v: overwrite must be a repeatable path flag: %v", spelling, err)
		}
		if want := []string{"workharbor.json", "projects/a,b.json"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%v: got %q, want %q", spelling, got, want)
		}
	}
}
