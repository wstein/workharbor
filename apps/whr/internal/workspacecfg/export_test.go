// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

// SetGitWalkBudget lowers the entry budget of the .git walk for a test and
// returns the function that restores it.
func SetGitWalkBudget(n int) (restore func()) {
	old := maxGitWalkEntries
	maxGitWalkEntries = n
	return func() { maxGitWalkEntries = old }
}
