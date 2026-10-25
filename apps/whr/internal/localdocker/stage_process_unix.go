// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"os"
	"sync"
	"syscall"
	"time"
)

// The leader remains waitable until releaseBeforeReap. Holding the mutex across
// every signal and disabling the timer before Wait prevents a reused PID/group
// from becoming a cleanup target. This is separate from discovery's old runner.
type stageProcessGroup struct {
	mu     sync.Mutex
	owned  bool
	timer  *time.Timer
	signal func(syscall.Signal) error
}

func (g *stageProcessGroup) terminate() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.owned {
		return os.ErrProcessDone
	}
	e := g.signal(syscall.SIGTERM)
	if g.timer == nil {
		g.timer = time.AfterFunc(2*time.Second, func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.owned {
				_ = g.signal(syscall.SIGKILL)
			}
		})
	}
	return e
}
func (g *stageProcessGroup) releaseBeforeReap(_ bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.timer != nil {
		g.timer.Stop()
	}
	if g.owned {
		_ = g.signal(syscall.SIGKILL)
	}
	g.owned = false
}
