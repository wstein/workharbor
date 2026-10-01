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
| `CLI.dc.html` | A `whr` terminal session: `ls`, `inbox`, `approve` (including a push), `usage`, `purge` and exit codes |

`canvas.json` holds the layout of the artboards on the canvas.

## Format

These are the source files of a Claude Design canvas. Each `.dc.html` artboard loads the canvas runtime through `./support.js`, which is not part of this repository, so the files render only inside that editor and not when opened directly in a browser.

The mock follows the design in [`docs/content/docs/design/_index.md`](../../docs/content/docs/design/_index.md). In v0 the web UI is a remote for the agent (live chat, send, start, pause, resume, cancel, decisions and approvals). What is planned for after v0 (open in editor, takeover) carries an "after v0" mark. Host checks that spike #2 did not measure are marked "Not tested". The design document is the source of truth; the mock is an illustration and may lag behind it.

## Not yet in the mock

The text and example data follow the design as of D43: workspaces with named agents and `agent/<role>` branches (D42), sign-in inside the environment (D40), the rebase-conflict question, and the API key in a `0600` file (D41). These need new layout in Claude Design and are not drawn yet:

- **A 12-inch tablet artboard** (D35): reviewing a topic before "Ready to push?" with the diff beside the commits, and supervising several agents of a workspace side by side.
- **A Workspaces view**: each workspace with its environment, its named agents (role, branch, current task, state) and an "Open console" action (D42, D43).
- **The console** (D43): a terminal in the console environment with the workspaces mounted read-only and one read-write, reached from the web app and from `whr console`.
- **Sign-in inside the environment** (D40): the console showing the agent's own login, with no credential passing through workharbor's pages.

