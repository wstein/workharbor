# Feature implementation quickstart

The feature is not runnable yet. Start with T001; then follow tasks in dependency
order. For existing baseline prerequisite inspection:

```sh
SPECIFY_FEATURE=001-workspace-lifecycle .specify/scripts/bash/check-prerequisites.sh --json --require-tasks --include-tasks
make check
```

On a separate implementation branch use `git switch -c 001-workspace-lifecycle`.
Use Codex skills `$speckit-analyze` and `$speckit-implement`. Record real application
startup and browser commands here when implemented; no placeholder passing demo.
