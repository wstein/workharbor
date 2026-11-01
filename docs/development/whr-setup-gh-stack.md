# `gh-stack` setup step

Issue #310. Native GitHub stacked pull requests use the `gh stack` extension
(`github/gh-stack`) of the GitHub CLI. The library `apps/whr/internal/hostsetup/ghext`
offers it as a setup item. It is not registered in a command yet: `whr setup` is
registered by Feature 009 T010/T011 (`host.setup` is `later` in the command table), and the
host catalogue is Homebrew-only with exactly 13 entries (Feature 009 FR-009, FR-010, T001).
The wizard calls `ghext.Describe()` and `ghext.Run()` when it is built.

## Behavior

- Item: id `gh-stack`, preselected, individually skippable, requires `gh`.
- Order: deselected means no `gh` call. Then check that `gh` is on PATH, run
  `gh extension list`, and match the exact repository `github/gh-stack` in column 2. The list is
  TAB-separated (name, repository, version; the name may contain a space, e.g. `gh stack`),
  measured on gh 2.103.3 with gh-stack v0.3.0. Space-separated output is not recognized.
- Installed: report the version, run nothing, ask nothing (no second install).
- Missing: show the step `gh extension install github/gh-stack` (it changes the user's `gh`
  profile) before running it. Interactive runs need a yes. Without a terminal only the
  explicit yes flag of the caller confirms; otherwise the result is `blocked`
  (`confirmation_required`). After the install the list is read again and the version is reported.
- Without `gh`: result `blocked` (`gh_missing`) with a message; nothing else runs.
- Without network: result `failed` (`network_unavailable`) with a message. Other failures are
  `install_failed`, `detect_failed` or `not_detected_after_install`. No panic, no traceback.
- Host and container run the same code; there is no platform branch.

## Tests

All `gh` calls go through the `GH` port; tests use a fake. The real adapter `ExecGH` is tested
with an injected command runner. No test installs an extension.
`go test ./internal/hostsetup/ghext/` from `apps/whr`.
