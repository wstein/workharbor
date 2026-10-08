# App mock

Interactive mock of the workharbor web UI and the `whr` CLI, made as a customer and designer demo. All data is example data.

| Artboard | Shows |
| --- | --- |
| `Main.dc.html` | Harbor overview: task list with state filters (including a failed task), usage summary with the five-hour and seven-day windows, capacity, agent logins (Codex CLI in degraded mode), policy with its fixed floor |
| `Task.dc.html` | Task detail as a live chat: transcript, usage and cost, permission modes, a live tool approval with deadline and truncated input, pause as a hard interrupt, history controls (clear view, purge), branch and push (the supervisor pushes after "Ready to push?"), workspace with tool store, volume and network |
| `Inbox.dc.html` | Inbox of questions, tool approvals with deadlines, plan approvals, a "Ready to push?" review for a commit SHA, an expired login (sign in again inside the environment, D40), a rebase conflict with its three answers (§4.2) and an expired approval |
| `Mobile.dc.html` | Phone inbox, decisions first, with the installable-app hint and a compact usage meter |
| `Chat.dc.html` | Phone chat: live transcript, approve or deny, message the agent |
| `Push.dc.html` | Phone push notifications (ntfy): task ID, event kind and a link only |
| `Onboarding.dc.html` | First-run web wizard (after v0: release 1 onboards through `whr login`, `whr doctor` and a config file, §9.5): sign in, install the workharbor GitHub App and verify the forge limits, agents and default mode, host checks, ntfy |
| `Setup.dc.html` | Read-only setup status page (`/setup`), as states: all ok, a FAIL with its fix as a bare command block (as `whr doctor` prints it: blank line above and below, no `$`, 4-space indent), the unverified list including Tailscale, a running check with live rows, the redacted report download, the guarded "run user-phase fixes" button with its confirmation modal (deferred), the host-phase card (the browser never runs host steps) and the final "What you need to do now" block of `whr setup host` (flags `--yes`, `--dry-run`, `--only`, `--from`, `--dev`; the wording of lines the goldens do not cover is marked UNVERIFIED); the fail-count badge on the Main link is static |
| `CLI.dc.html` | A `whr` terminal session: `ls`, `inbox`, `approve` (including a push), `usage`, `purge` and exit codes |

`canvas.json` holds the layout of the artboards on the canvas.

## Format

These are the source files of a Claude Design canvas. Each `.dc.html` artboard loads the canvas runtime through `./support.js`, which is not part of this repository, so the files render only inside that editor and not when opened directly in a browser.

The mock follows the design in [`docs/content/docs/design/_index.md`](../../docs/content/docs/design/_index.md). In v0 the web UI is a remote for the agent (live chat, send, start, pause, resume, cancel, decisions and approvals). What is planned for after v0 (open in editor, takeover) carries an "after v0" mark. Host checks that spike #2 did not measure carry the status `not_verified` in the report JSON; `whr doctor` and `whr setup` print it as `? unverified`, and a missing Tailscale is `unverified`, never `FAIL`. The design document is the source of truth; the mock is an illustration and may lag behind it.

## Light theme

`Setup.dc.html`, `Main.dc.html` and `Onboarding.dc.html` have a light variant with the same colour semantics (ok, WARN, FAIL, unverified, skip); the other artboards are dark only. Every colour in these three files is a CSS custom property (token) with its dark value as the fallback, defined once in each file's `<style>` block. Dark stays the default. Light is used when the system asks for it (`prefers-color-scheme: light`) or when the "Light / dark" button in the top right corner of the artboard is pressed (it sets `data-theme` on the root element and overrides the system either way). Layout, text, step names and commands are unchanged. Status words keep their symbol and their text label (`✓ ok`, `! WARN`, `✗ FAIL`, `? unverified`, `- skip`); colour is never the only signal.

Contrast, computed with WCAG 2.x relative luminance (a small script outside the repository). Text pairs need 4.5:1; the pairs marked 3:1 are non-text parts (accent, status colours, borders and track colours). All computed pairs pass.

| Foreground | Background | Ratio | Needs | Result |
| --- | --- | --- | --- | --- |
| fg #0f1b2d | bg-page #f3f6fb | 15.96 | 4.5:1 | pass |
| fg #0f1b2d | bg-panel #ffffff | 17.28 | 4.5:1 | pass |
| fg #0f1b2d | bg-head #eef2f8 | 15.38 | 4.5:1 | pass |
| fg #0f1b2d | bg-side #e8edf5 | 14.70 | 4.5:1 | pass |
| fg #0f1b2d | bg-active #d6e4fb | 13.46 | 4.5:1 | pass |
| fg-2 #26364d | bg-page #f3f6fb | 11.29 | 4.5:1 | pass |
| fg-2 #26364d | bg-panel #ffffff | 12.23 | 4.5:1 | pass |
| fg-2 #26364d | bg-head #eef2f8 | 10.88 | 4.5:1 | pass |
| fg-2 #26364d | bg-side #e8edf5 | 10.40 | 4.5:1 | pass |
| fg-2 #26364d | bg-active #d6e4fb | 9.52 | 4.5:1 | pass |
| fg-muted #475a73 | bg-page #f3f6fb | 6.51 | 4.5:1 | pass |
| fg-muted #475a73 | bg-panel #ffffff | 7.05 | 4.5:1 | pass |
| fg-muted #475a73 | bg-head #eef2f8 | 6.28 | 4.5:1 | pass |
| fg-muted #475a73 | bg-side #e8edf5 | 6.00 | 4.5:1 | pass |
| fg-muted #475a73 | bg-active #d6e4fb | 5.49 | 4.5:1 | pass |
| fg-faint #566a84 | bg-panel #ffffff | 5.54 | 4.5:1 | pass |
| fg-faint #566a84 | bg-page #f3f6fb | 5.11 | 4.5:1 | pass |
| fg-strong #0b1626 | bg-active #d6e4fb | 14.13 | 4.5:1 | pass |
| fg-strong #0b1626 | bg-panel #ffffff | 18.15 | 4.5:1 | pass |
| fg-2 #26364d | bg-active #d6e4fb | 9.52 | 4.5:1 | pass |
| ok #166534 | bg-page #f3f6fb | 6.58 | 4.5:1 | pass |
| ok #166534 | bg-panel #ffffff | 7.13 | 4.5:1 | pass |
| ok #166534 | bg-head #eef2f8 | 6.35 | 4.5:1 | pass |
| warn #7a4a00 | bg-page #f3f6fb | 6.91 | 4.5:1 | pass |
| warn #7a4a00 | bg-panel #ffffff | 7.48 | 4.5:1 | pass |
| warn #7a4a00 | bg-head #eef2f8 | 6.66 | 4.5:1 | pass |
| fail #b42318 | bg-page #f3f6fb | 6.07 | 4.5:1 | pass |
| fail #b42318 | bg-panel #ffffff | 6.57 | 4.5:1 | pass |
| fail #b42318 | bg-head #eef2f8 | 5.85 | 4.5:1 | pass |
| unv #1d4ed8 | bg-page #f3f6fb | 6.19 | 4.5:1 | pass |
| unv #1d4ed8 | bg-panel #ffffff | 6.70 | 4.5:1 | pass |
| unv #1d4ed8 | bg-head #eef2f8 | 5.96 | 4.5:1 | pass |
| run #1d4ed8 | bg-page #f3f6fb | 6.19 | 4.5:1 | pass |
| run #1d4ed8 | bg-panel #ffffff | 6.70 | 4.5:1 | pass |
| run #1d4ed8 | bg-head #eef2f8 | 5.96 | 4.5:1 | pass |
| accent #0f766e | bg-page #f3f6fb | 5.05 | 4.5:1 | pass |
| accent #0f766e | bg-panel #ffffff | 5.47 | 4.5:1 | pass |
| accent #0f766e | bg-head #eef2f8 | 4.87 | 4.5:1 | pass |
| ok #166534 | bg-ok #dcf5e4 | 6.19 | 4.5:1 | pass |
| warn #7a4a00 | bg-warn #fdefc7 | 6.54 | 4.5:1 | pass |
| fail #b42318 | bg-fail #fde0e0 | 5.30 | 4.5:1 | pass |
| fail #b42318 | bg-fail-2 #fde0e3 | 5.31 | 4.5:1 | pass |
| unv #1d4ed8 | bg-unv #e0eafc | 5.53 | 4.5:1 | pass |
| run #1d4ed8 | bg-run #dbeafe | 5.49 | 4.5:1 | pass |
| accent #0f766e | bg-review #d3f4ee | 4.68 | 4.5:1 | pass |
| fg-muted #475a73 | bg-queued #e6ebf3 | 5.89 | 4.5:1 | pass |
| fg-muted #475a73 | bg-skip #e6ebf3 | 5.89 | 4.5:1 | pass |
| fg-faint #566a84 | bg-queued #e6ebf3 | 4.63 | 4.5:1 | pass |
| on-accent #ffffff | accent #0f766e | 5.47 | 4.5:1 | pass |
| on-warn #ffffff | warn #7a4a00 | 7.48 | 4.5:1 | pass |
| on-fail #ffffff | fail #b42318 | 6.57 | 4.5:1 | pass |
| accent #0f766e | bg-panel #ffffff | 5.47 | 3:1 | pass |
| accent #0f766e | bg-active #d6e4fb | 4.26 | 3:1 | pass |
| ok #166534 | bg-panel #ffffff | 7.13 | 3:1 | pass |
| warn #7a4a00 | bg-panel #ffffff | 7.48 | 3:1 | pass |
| fail #b42318 | bg-panel #ffffff | 6.57 | 3:1 | pass |
| line #7a8ba3 | bg-panel #ffffff | 3.47 | 3:1 | pass |
| line #7a8ba3 | bg-page #f3f6fb | 3.20 | 3:1 | pass |

Not computed, therefore unverified:

- The toggle and the `prefers-color-scheme` switch were not run in the Claude Design editor (`support.js` is not in this repository); the token blocks and the markup were checked only for balanced tags.
- The non-default accent colours offered by the editor (`#60a5fa`, `#fbbf24`, `#f472b6`) are not themed and not checked on the light backgrounds; the white text on the accent button is verified for the default accent only.
- Pairs not listed above (for example `--fg-muted` on `--bg-ok` or `--bg-unv`, and the hover and disabled looks), and the decorative separator colour `--line-soft`, which carries no information.
- The dark values are the accepted mock's own and were not re-computed.

## Not yet in the mock

The text and example data follow the design as of D43: workspaces with named agents and `agent/<role>` branches (D42), sign-in inside the environment (D40), the rebase-conflict question, and the API key in a `0600` file (D41). These need new layout in Claude Design and are not drawn yet:

- **A 12-inch tablet artboard** (D35): reviewing a topic before "Ready to push?" with the diff beside the commits, and supervising several agents of a workspace side by side.
- **A Workspaces view**: each workspace with its environment, its named agents (role, branch, current task, state) and an "Open console" action (D42, D43).
- **The console** (D43): a terminal in the console environment with the workspaces mounted read-only and one read-write, reached from the web app and from `whr console`.
- **Sign-in inside the environment** (D40): the console showing the agent's own login, with no credential passing through workharbor's pages.

