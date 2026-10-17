// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

// GitRunner runs the fixed Git invocations; the production runner executes the
// binary and tests inject a fake.
type GitRunner interface{}

func (r *run) git() {
	if r.opt.GitFree {
		return
	}
	if _, aerr := r.d.lstat(".git"); aerr != nil && aerr.kind == accMissing {
		r.c.add("git.not_repository", "", "the directory contains no .git; Git checks did not run", "")
	}
}
