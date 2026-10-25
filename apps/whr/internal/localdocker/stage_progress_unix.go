//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// v0.89 emits structured start/stop events around the exact host argv, with
// startTimestamp correlating completion. Child output is encoded as raw events
// and cannot become an outer start/stop event. No command body is reported.
type stageInitializeProgress struct {
	mu                         sync.Mutex
	pending                    map[string]int
	running                    map[string]int
	remaining                  int
	buffer                     []byte
	timer                      *time.Timer
	cancel                     context.CancelFunc
	hostBudget, resourceBudget time.Duration
	started, complete, closed  bool
	failure                    string
}

func newStageInitializeProgress(labels []string, host, resource time.Duration, cancel context.CancelFunc) *stageInitializeProgress {
	p := &stageInitializeProgress{pending: map[string]int{}, running: map[string]int{}, remaining: len(labels), cancel: cancel, hostBudget: host, resourceBudget: resource}
	for _, label := range labels {
		p.pending[label]++
	}
	p.arm(host, "initializer progress unavailable or deadline exceeded")
	return p
}
func (p *stageInitializeProgress) arm(duration time.Duration, reason string) {
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(duration, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if !p.closed {
			p.failure = reason
			p.cancel()
		}
	})
}
func (p *stageInitializeProgress) write(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.complete || p.failure != "" {
		return
	}
	p.buffer = append(p.buffer, data...)
	for {
		n := bytes.IndexByte(p.buffer, '\n')
		if n < 0 {
			return
		}
		line := p.buffer[:n]
		p.buffer = p.buffer[n+1:]
		var event struct {
			Type, Text                string
			Timestamp, StartTimestamp int64
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		switch event.Type {
		case "start":
			if p.pending[event.Text] == 0 {
				continue
			}
			if !p.started {
				p.started = true
				p.arm(p.hostBudget, "initializer deadline exceeded")
			}
			p.pending[event.Text]--
			p.running[fmt.Sprintf("%d\x00%s", event.Timestamp, event.Text)]++
		case "stop":
			key := fmt.Sprintf("%d\x00%s", event.StartTimestamp, event.Text)
			if p.running[key] == 0 {
				continue
			}
			p.running[key]--
			p.remaining--
			if p.remaining == 0 {
				p.complete = true
				p.arm(p.resourceBudget, "resource deadline exceeded")
				return
			}
		}
	}
}
func (p *stageInitializeProgress) close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
	}
	if p.failure != "" {
		return stageConflict(p.failure)
	}
	if !p.complete {
		return stageConflict("initializer progress unavailable or ambiguous")
	}
	return nil
}
func stageInitializeLabels(command any) ([]string, error) {
	labels := []string{}
	var add func(any) error
	add = func(command any) error {
		var args []string
		switch command := command.(type) {
		case string:
			if strings.TrimSpace(command) == "" {
				return nil
			}
			args = []string{"/bin/sh", "-c", command}
		case []any:
			for _, v := range command {
				s, ok := v.(string)
				if !ok {
					return stageConflict("initializer argv type unsupported")
				}
				args = append(args, s)
			}
		case nil:
			return nil
		default:
			return stageConflict("initializer command shape unsupported")
		}
		if len(args) > 0 {
			labels = append(labels, "Run: "+strings.Split(strings.Join(args, " "), "\n")[0])
		}
		return nil
	}
	if commands, ok := command.(map[string]any); ok {
		keys := []string{}
		for key := range commands {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := add(commands[key]); err != nil {
				return nil, err
			}
		}
	} else if err := add(command); err != nil {
		return nil, err
	}
	return labels, nil
}
