// SPDX-License-Identifier: EUPL-1.2

// Package ptytest drives interactive programs through a real pseudo-terminal
// in tests (task T031). It exists so later work items, first the init wizard
// (T032), can script an end-to-end terminal conversation without sleeps,
// network, user configuration or leaked processes.
//
// # Usage
//
// A test binary re-executes itself to run a function under the PTY. Call Main
// from TestMain with the table of functions, then drive them from tests:
//
//	func TestMain(m *testing.M) { ptytest.Main(m, map[string]func([]string) int{
//		"wizard": func(args []string) int { return runWizard(args) },
//	}) }
//
//	func TestWizard(t *testing.T) {
//		s := ptytest.StartFunc(t, "wizard", ptytest.Options{Rows: 24, Cols: 80})
//		s.Expect("Name: ")
//		s.Send("demo")
//		s.SendKey(ptytest.KeyEnter)
//		s.SendKey(ptytest.KeyCtrlC)
//		s.ExpectExit(72)
//		s.AssertNoRawControls() // untrusted text never reached the terminal
//	}
//
// Start runs any executable (for example a compiled whr) the same way.
//
// # Behavior
//
//   - The child is a session leader with the PTY as controlling terminal and
//     as stdin, stdout and stderr, so Ctrl-C raises SIGINT in its process
//     group and window-size changes (Resize) raise SIGWINCH.
//   - Expect and friends wait for output with a deadline (Options.Timeout, or
//     the explicit *Within variants). There are no sleeps; a timeout, output
//     that ends first (fail fast, no waiting for the deadline) or an exceeded
//     Options.MaxOutput fails the test with a bounded message (first and last
//     2 KiB of the transcript plus its length). Expect matches visible text
//     with escape sequences removed and consumes output up to the end of the
//     match, so consecutive Expect calls may match consecutive lines. Work is
//     linear in the output: new output is projected once; ExpectRegexp looks at
//     the last 64 KiB of unconsumed text; ExpectScreen at the visible rows plus
//     the last 256 scrolled-off lines.
//   - Screen renders the whole transcript on a Rows x Cols terminal model:
//     carriage return, backspace, tab, erase in line and display, cursor
//     movement and save/restore (ESC 7/8, CSI s/u, A-H, d, f), insert and
//     delete of lines and characters, deferred line wrap at the width,
//     scrolling with history, wide (two-cell) and combining characters and the
//     alternate screen (?1047/?1049/?47). Colors, titles and other sequences
//     are ignored. Scroll regions, tab stops, double-width line attributes and
//     bidirectional text are not modeled. A C0 control byte inside a CSI ends
//     that sequence as malformed (a VT parser would execute it and go on). Transcript is the raw byte stream.
//   - AssertNoRawControls waits for the child to exit and its output to end,
//     then fails if the transcript holds any control character (FR-022
//     classes), escape sequence or invalid UTF-8 byte except newline, carriage
//     return, tab and explicitly allowed literals (matched on whole tokens
//     only); a multibyte character cut off at the very end is tolerated.
//   - The environment is built from scratch: private HOME, XDG directories and
//     TMPDIR under t.TempDir, LC_ALL=C, TZ=UTC, a short PATH, the requested
//     TERM and optional NO_COLOR. Nothing is inherited from the test process
//     except GOCOVERDIR. Add variables through Options.Env.
//   - When the child exits the whole process group is swept once with SIGKILL
//     (so descendants cannot outlive it and the group is never signalled after
//     its leader was reaped), and cleanup kills the group if the child still
//     runs. No child or grandchild outlives the test. A descendant that
//     leaves the process group is out of reach.
//
// # Platforms
//
// Linux and macOS (build tags linux and darwin; /dev/ptmx is allocated with
// the standard library syscall package, no cgo and no third-party module).
// On any other platform Start and StartFunc skip the test with a message that
// contains "SKIPPED, NOT PASSED"; a skipped test is not evidence. Linux is
// exercised on aarch64 and x86_64 (the latter under emulation here). The macOS
// code only compiles (GOOS=darwin vet and test compile) and its ioctl numbers
// are untested: acceptance evidence SC-007 needs a real run on macOS with the
// exact versions recorded; no CI run on macOS is claimed.
package ptytest
