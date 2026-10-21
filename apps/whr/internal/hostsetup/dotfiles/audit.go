// SPDX-License-Identifier: EUPL-1.2
package dotfiles

// DiffCounts reports full-file line changes from the same immutable bytes used
// by Preview and Apply, including when the displayed unified diff is truncated.
// It reveals no file contents or Git values.
func (p *Preview) DiffCounts() (added, removed int) {
	if p == nil || p.state == Identical {
		return 0, 0
	}
	return lines(p.content), lines(p.before.data)
}
