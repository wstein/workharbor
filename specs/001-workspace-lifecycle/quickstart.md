# Feature implementation quickstart

The feature is not runnable yet. Start with T001; then follow tasks in dependency
order. For existing baseline prerequisite inspection:

```sh
SPECIFY_FEATURE_DIRECTORY=specs/001-workspace-lifecycle SPECIFY_FEATURE_NO_PERSIST=1 .specify/scripts/bash/check-prerequisites.sh --json --require-tasks --include-tasks
make check
python3 scripts/select_feature.py 001-workspace-lifecycle
```

On a separate implementation branch use `git switch -c 001-workspace-lifecycle`.
Use Codex skills `$speckit-analyze` and `$speckit-implement`. Record real application
startup and browser commands here when implemented; no placeholder passing demo.
