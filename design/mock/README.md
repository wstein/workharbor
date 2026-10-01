# App mock

Interactive mock of the workharbor web UI and the `whr` CLI, made as a customer and designer demo. All data is example data.

| Artboard | Shows |
| --- | --- |
| `Main.dc.html` | Harbor overview: task list with filters, capacity, agent accounts, policy |
| `Task.dc.html` | Task detail as a live chat: transcript, permission modes, a live tool approval, history controls (clear view, purge), review candidate, workspace with tool store, volume and network |
| `Inbox.dc.html` | Inbox of questions, tool approvals, plan approvals, reviews and expired logins |
| `Mobile.dc.html` | Phone inbox, decisions first, with the installable-app hint |
| `Chat.dc.html` | Phone chat: live transcript, approve or deny, message the agent |
| `Push.dc.html` | Phone push notification (ntfy): task ID, event kind and a link only |
| `Onboarding.dc.html` | First-run wizard: sign in, forge, agents and default mode, host checks, ntfy |
| `CLI.dc.html` | A `whr` terminal session |

`canvas.json` holds the layout of the artboards on the canvas.

## Format

These are the source files of a Claude Design canvas. Each `.dc.html` artboard loads the canvas runtime through `./support.js`, which is not part of this repository, so the files render only inside that editor and not when opened directly in a browser.

The mock follows the design in [`docs/content/docs/design.md`](../../docs/content/docs/design.md). In v0 the web UI is a remote for the agent (live chat, send, start, pause, resume, cancel, decisions and approvals). What is planned for after v0 (open in editor, takeover) carries an "after v0" mark. Host checks that spike #2 did not measure are marked "Not tested". The design document is the source of truth; the mock is an illustration and may lag behind it.
