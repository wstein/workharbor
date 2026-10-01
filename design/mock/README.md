# App mock

Interactive mock of the workharbor web UI and the `whr` CLI, made as a customer and designer demo. All data is example data.

| Artboard | Shows |
| --- | --- |
| `Main.dc.html` | Harbor overview: task list with filters, capacity, agent accounts, policy |
| `Task.dc.html` | Task detail: activity log, decision, review candidate, workspace and session |
| `Inbox.dc.html` | Inbox of questions, approvals, reviews and expired logins |
| `Mobile.dc.html` | Phone inbox, decisions first |
| `Push.dc.html` | Phone push notification (ntfy): task ID, event kind and a link only |
| `Onboarding.dc.html` | First-run wizard |
| `CLI.dc.html` | A `whr` terminal session |

`canvas.json` holds the layout of the artboards on the canvas.

## Format

These are the source files of a Claude Design canvas. Each `.dc.html` artboard loads the canvas runtime through `./support.js`, which is not part of this repository, so the files render only inside that editor and not when opened directly in a browser.

The mock follows the design in [`docs/content/docs/design.md`](../../docs/content/docs/design.md). Where it shows features planned for after v0 (pause and take over, open in editor, new task, cancel), they carry an "after v0" mark. The design document is the source of truth; the mock is an illustration and may lag behind it.
