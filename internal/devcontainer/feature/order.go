package feature

import (
	"fmt"
	"strings"
)

// stripVersion turns ghcr.io/devcontainers/features/node:1 or ...@sha256:x into the
// reference without its version, which is what installsAfter names.
func stripVersion(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i >= 0 && !strings.Contains(ref[i:], "/") {
		ref = ref[:i]
	}
	return ref
}

// Order returns the indexes of the features in the order to install them: the order
// of the file, except that a feature comes after the ones its installsAfter names
// when those are in the file. Each position takes the earliest feature whose
// predecessors are done, so the file's order decides every tie. A cycle is an error;
// an installsAfter that names a feature the file does not have is returned in notes and
// never fetched.
func Order(refs []string, installsAfter [][]string) (order []int, notes []string, err error) {
	if len(refs) != len(installsAfter) {
		return nil, nil, fmt.Errorf("devcontainer feature: %d features and %d dependency lists", len(refs), len(installsAfter))
	}
	byRef := map[string]int{}
	for i, r := range refs {
		byRef[stripVersion(r)] = i
	}
	deps := make([][]int, len(refs))
	for i, ref := range refs {
		for _, d := range installsAfter[i] {
			j, ok := byRef[stripVersion(d)]
			switch {
			case !ok:
				notes = append(notes, fmt.Sprintf("%s: installsAfter %s is not among the features and is not fetched", ref, d))
			case j != i:
				deps[i] = append(deps[i], j)
			}
		}
	}
	done := make([]bool, len(refs))
	for len(order) < len(refs) {
		next := -1
		for i := range refs {
			if done[i] {
				continue
			}
			ready := true
			for _, j := range deps[i] {
				if !done[j] {
					ready = false
					break
				}
			}
			if ready {
				next = i
				break
			}
		}
		if next < 0 {
			var stuck []string
			for i := range refs {
				if !done[i] {
					stuck = append(stuck, refs[i])
				}
			}
			return nil, notes, fmt.Errorf("%w: the features %s wait on each other (installsAfter)", ErrRefused, strings.Join(stuck, ", "))
		}
		done[next] = true
		order = append(order, next)
	}
	return order, notes, nil
}
