---
title: First run
description: The path from a prepared Mac mini to the first task, with the commands in order.
weight: 5
toc: true
---

A short path through the manual, in order. **Every command is provisional and has not been run against a release yet** ({{< status unverified >}}); it was checked against `whr --help` of a build from `main` (issue #65). Each step names the page that holds the detail.

1. **Prepare the host**, as the administrator: [Prepare the Mac mini](host-setup.md).
2. **Install `whr`**, as the administrator: [Install, upgrade and release](install-upgrade-release.md). Then check it: `whr version`.
3. **Set up** the `whr` user's part, in its desktop session, not over SSH: `whr setup` (and `whr setup host` as the administrator). `whr setup --dry-run` shows every fix first. The GitHub App comes from `whr github app create`, see [Prepare the Mac mini](host-setup.md).
4. **Check** with `whr doctor`; fix what it reports, and treat "not verified" as not ok.
5. **Start the supervisor**: `whr service install`, then `whr service status`. See [Run the supervisor](run-the-supervisor.md).
6. **Add a workspace and an agent**: `whr ws add <name> --path <folder> --repo <owner/name> --role <role>`. It seeds the clone and starts the environment, which takes a while.
7. **Sign in to the agent** inside its environment yourself (a subscription login is never put in a file for `whr`): [Agent vendor terms](vendor-terms.md).
8. **Start a task**: `whr run <issue-url> --agent <workspace>/<role>`, then `whr logs <task> -f`, `whr inbox` and `whr approve`. See [Daily use](daily-use.md).
9. **Before you rely on it**, read [Security notes](security.md).
