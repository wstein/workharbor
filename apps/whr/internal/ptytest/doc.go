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
//     the explicit *Within variants). There are no sleeps; a timeout fails the
//     test and reports the escaped transcript. Expect matches visible text
//     with escape sequences removed and consumes output up to the end of the
//     match, so consecutive Expect calls may match consecutive lines.
//   - Screen renders the whole transcript the way a simple terminal would
//     (carriage return, backspace, erase-in-line, cursor movement). It does
//     not wrap lines or scroll. Transcript is the raw byte stream.
//   - AssertNoRawControls fails if the transcript holds any control character
//     (FR-022 classes) except newline, carriage return, tab and the explicitly
//     allowed literal sequences, proving untrusted text was made inert.
//   - The environment is built from scratch: private HOME, XDG directories and
//     TMPDIR under t.TempDir, LC_ALL=C, TZ=UTC, a short PATH, the requested
//     TERM and optional NO_COLOR. Nothing is inherited from the test process
//     except GOCOVERDIR. Add variables through Options.Env.
//   - Cleanup kills the whole process group with SIGKILL and waits for it, so
//     no child or grandchild outlives the test.
//
// # Platforms
//
// Linux and macOS (build tags linux and darwin; /dev/ptmx is allocated with
// the standard library syscall package, no cgo and no third-party module).
// On any other platform Start and StartFunc skip the test with a message that
// contains "SKIPPED, NOT PASSED"; a skipped test is not evidence. Linux is
// exercised on aarch64 and x86_64; macOS compiles (GOOS=darwin vet) but must
// be run on a Mac for acceptance.
package ptytest
