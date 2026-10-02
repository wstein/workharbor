package service

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain fails the package if a test leaves a service's background goroutine
// behind: the board's worker once outlived its service in every test that used it,
// and the pile starved `make race`. A service is stopped by Shutdown, which every
// rig registers with t.Cleanup.
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		if leaked := leakedWorkers(); leaked != "" {
			fmt.Fprintf(os.Stderr, "service tests leaked goroutines of a stopped service:\n%s\n", leaked)
			code = 1
		}
	}
	os.Exit(code)
}

// leakedWorkers returns the stacks of the goroutines that belong to a service and
// should be gone, after giving the ones that are returning a moment.
func leakedWorkers() string {
	var found string
	for range 40 {
		buf := make([]byte, 4<<20)
		buf = buf[:runtime.Stack(buf, true)]
		found = ""
		for _, g := range strings.Split(string(buf), "\n\n") {
			if strings.Contains(g, "(*Service).boardWorker") {
				found += g + "\n\n"
			}
		}
		if found == "" {
			return ""
		}
		time.Sleep(25 * time.Millisecond)
	}
	return found
}
