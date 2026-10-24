# SC-009 checkpoint B — OWNER result template

Status: **UNVERIFIED**. No live Herdr, Docker, SSH, installation or enabling was
performed by the agent. Offline fakes and SC-010 cannot close this record.

After accepted P4 and separate owner authorization, run checkpoint B in
[quickstart](../../../specs/010-host-and-workspace-panes/quickstart.md).


Record exact accepted product/plugin SHAs, operator/authorization, host OS,
Docker client/server, Herdr version, notebook terminal/font, controlled fixture,
background enabled/disabled and sanitized evidence. Never capture session contents
or credential-bearing environment. Repeat the base checkpoint A too.

| Check | Expected | Observed |
| --- | --- | --- |
| Individual ctrl-p/q/space/\\/] | Each reaches controlled shell fixture | NOT RUN |
| ctrl-] ctrl-^ detach | Client detaches; fixture shell survives | NOT RUN |
| Host-parent exit/status and resize | Status preserved; terminal usable | NOT RUN |
| SSH drop/reconnect | Same pane, command, directory, shell state | NOT RUN |
| Workspace/host identity | Glyph text in pane title AND tab label | NOT RUN |
| Clear and narrow tab labels | Persistent identity; acceptable notebook rendering | NOT RUN |
| Shared-tab split | Record latest shared label and each pane's identity | NOT RUN |
| Background disabled | Identity retained; no plugin OSC 11 | NOT RUN |
| Background enabled | Only opened pane tinted; neighbors/tab/outer terminal unchanged | NOT RUN |

Owner acceptance link: PENDING. Record failures and untested optional cases
explicitly; do not infer installation or real-host acceptance from a green PR.
