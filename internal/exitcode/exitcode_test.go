package exitcode

import (
	"errors"
	"fmt"
	"testing"
)

type coded int

func (c coded) Error() string { return fmt.Sprintf("coded %d", int(c)) }
func (c coded) ExitCode() int { return int(c) }

func TestFrom(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, OK},
		{"plain error", errors.New("boom"), Error},
		{"coded error", coded(Conflict), Conflict},
		{"wrapped coded error", fmt.Errorf("task t1: %w", coded(NotFound)), NotFound},
		{"joined errors take the first coded one", errors.Join(errors.New("x"), coded(Auth)), Auth},
		{"coded error deep in a chain", fmt.Errorf("a: %w", fmt.Errorf("b: %w", coded(Timeout))), Timeout},
	}
	for _, tc := range tests {
		if got := From(tc.err); got != tc.want {
			t.Errorf("%s: From = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestCodesAreStable(t *testing.T) {
	// These numbers are part of the scripting contract (design §9.2).
	want := map[string]int{
		"OK": 0, "Error": 1, "Usage": 2, "NotFound": 3, "Auth": 4,
		"Conflict": 5, "NeedsHuman": 6, "Timeout": 7, "Quit": 8, "TaskFailed": 10,
	}
	got := map[string]int{
		"OK": OK, "Error": Error, "Usage": Usage, "NotFound": NotFound, "Auth": Auth,
		"Conflict": Conflict, "NeedsHuman": NeedsHuman, "Timeout": Timeout, "Quit": Quit, "TaskFailed": TaskFailed,
	}
	for name, code := range want {
		if got[name] != code {
			t.Errorf("%s = %d, want %d", name, got[name], code)
		}
	}
}
