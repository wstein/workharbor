// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"fmt"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// maxEcho bounds how much of an unknown word an error repeats.
const maxEcho = 64

// unknown builds the usage error for an unknown word typed after parent
// (canonical group path, empty for the top level), with a one-line hint
// naming the nearest valid spelling (FR-053).
func unknown(parent []string, typed string) *exitcode.Error {
	word := typed
	if r := []rune(word); len(r) > maxEcho {
		word = string(r[:maxEcho]) + "..."
	}
	msg := fmt.Sprintf("unknown command %q", word)
	if s := nearest(parent, typed); s != "" {
		msg += fmt.Sprintf("; did you mean %q?", "whr "+s)
	}
	return exitcode.Errorf(exitcode.PathUnknownCommand, "%s", msg)
}

// nearest returns the closest valid spelling below parent, or "".
func nearest(parent []string, typed string) string {
	type candidate struct{ name, full string }
	var cands []candidate
	add := func(name string, full []string) { cands = append(cands, candidate{name, joinPath(full)}) }
	seen := map[string]bool{}
	for _, e := range entries {
		switch {
		case len(parent) == 0:
			first := e.Path[0]
			if !seen[first] {
				seen[first] = true
				add(first, []string{first})
			}
			for _, a := range groupAliases[first] {
				if !seen[a] {
					seen[a] = true
					add(a, []string{a})
				}
			}
			if e.Short != nil && !seen[e.Short[0]] {
				seen[e.Short[0]] = true
				add(e.Short[0], e.Short[:1])
			}
		case len(e.Path) > len(parent) && equal(e.Path[:len(parent)], parent):
			next := e.Path[len(parent)]
			if !seen[next] {
				seen[next] = true
				add(next, e.Path[:len(parent)+1])
			}
		}
	}
	best, bestDist := "", 3
	for _, c := range cands {
		if d := distance(typed, c.name); d < bestDist && d < len([]rune(typed)) {
			best, bestDist = c.full, d
		}
	}
	return best
}

func equal(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }

// distance is the Levenshtein distance between a and b in runes.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
