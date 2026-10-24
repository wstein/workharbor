# T030 custom projects path refusals

This test-only carry follows the [CLEAR review of merged PR #243](https://github.com/wstein/workharbor/pull/243#pullrequestreview-5639986913)
and the [custom placeholder record](t030-custom-projects-gitkeep-20261024.md).
Measured on 2026-10-24 from base `86008fab4b0131187c3d3b244fef94a48063b48b`
with Go `go1.27.2 linux/arm64`, umask 022 and the existing production guards.
No production change or whole-T030 acceptance is claimed.

At this record's audited base `86008fab4b0131187c3d3b244fef94a48063b48b`, 29 of T030's 31 literal test names are declared and two are absent: `TestInitNoGitNoNetwork` (pending [PR #249](https://github.com/wstein/workharbor/pull/249)) and `TestInitDevcontainerFlagNotInRelease` (superseded by accepted T033 selection support); the older core record's 17-name gap is historical.

## Bounded assertions

[TestInitSymlinkTarget](../../../apps/whr/internal/scaffold/init_acceptance_paths_test.go)
now includes four `custom-projects` subcases. An existing manifest with
`projects_path` escaping through `../`, an absolute path, a symlink at the
nested `source/components` leaf, or a symlink at its `source` ancestor must
refuse discovery before preview with exit 68, even with manifest overwrite
consent. The first two exercise existing-manifest validation; the latter two
exercise no-follow discovery, consistent with FR-057(a/b).

Each subcase owns an external temporary directory with a nonempty `.gitkeep`
and a nested owner file. Before and after discovery, complete inventories of
both the external directory and workspace compare names, device/inode identity
via `os.SameFile`, modes, sizes, modification times via `Time.Equal`, regular
file bytes and symlink destinations. This checks that refusal neither creates
nor removes entries, changes the manifest, replaces links nor touches external
sentinels. Reads can change access times; access time is outside this assertion.
All filesystem effects are confined to test-owned temporary fixtures.

The existing custom placeholder no-op test also uses `Time.Equal` for its
modification-time comparison. The core record gains one forward pointer to the
merged placeholder evidence; its historical measurements and counts stay intact.

## Fresh mutation RED and unchanged-production GREEN

The first execution of the new subcases used a compiler overlay on
[discover.go](../../../apps/whr/internal/scaffold/discover.go). The mutation
replaces the authored projects path with `projects` before manifest validation
and target discovery. It models default-path substitution bypassing the custom
path checks, without modifying tracked production source. Reproduce from the
repository root:

```sh
umask 022
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
python3 - <<'PYCODE'
import json
import tempfile
from pathlib import Path

source = Path("apps/whr/internal/scaffold/discover.go").resolve()
mutation = Path(tempfile.mkdtemp(prefix="whr-custom-path-mutation-"))
text = source.read_text().replace('"context"', '"context"\n\t"encoding/json"', 1)
old = "m := workspacecfg.CheckManifest(entry.Data)"
new = '''var authored map[string]json.RawMessage
        if err := json.Unmarshal(entry.Data, &authored); err != nil {
            return s, discoveryConflict()
        }
        authored["projects_path"] = json.RawMessage(`"projects"`)
        normalized, err := json.Marshal(authored)
        if err != nil {
            return s, discoveryConflict()
        }
        m := workspacecfg.CheckManifest(normalized)'''
assert text.count(old) == 1
mutant = mutation / "discover.go"
mutant.write_text(text.replace(old, new, 1))
overlay = mutation / "overlay.json"
overlay.write_text(json.dumps({"Replace": {str(source): str(mutant)}}))
print(overlay)
PYCODE
```

Use the printed absolute overlay filename for `<overlay.json>` below. Run from
`apps/whr`, retaining the exported environment:

| Command | Measured result |
| --- | --- |
| `go test -count=1 -overlay=<overlay.json> -run '^TestInitSymlinkTarget/custom-projects/' -v ./internal/scaffold` | Exit 1, all four new subcases fail at the exit-68 assertion because discovery incorrectly returns nil. Compilation succeeds; no setup failures or skips. |
| `go test -count=1 -overlay=<overlay.json> -run '^TestInitSymlinkTarget$/^(workharbor[.]json\|[.]devcontainer)$' -v ./internal/scaffold` | Exit 0, both pre-existing manifest/devcontainer symlink subcases pass, exposing the previous coverage gap. |
| `go test -count=1 -run '^(TestInitSymlinkTarget\|TestInitGitkeepUnderProjectsPath)$' -v ./internal/scaffold` | Exit 0, both affected top-level tests and all eight subcases pass with unchanged production source; no failures or skips. |

The same overlay control was initially selected with a slash-containing regular
expression that ran only the manifest subcase; it passed. The corrected control
above explicitly ran and passed both pre-existing subcases. This is fresh
mutation evidence for a regression test, not historical failing-first proof
for the original implementation.

## Checks and limits

From `apps/whr`, `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go vet ./internal/scaffold`
passed. The complete affected package, `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -count=1 ./internal/scaffold`,
also passed (exit 0, 21.188 seconds) before handoff; this was a source test run
without race instrumentation. From the repository root, `gofmt -l` on both affected Go files produced
no output, `python3 -I -B scripts/check_repo.py`, `typos` on the four changed
files, and `git diff --check` passed. The source checker is a bootstrap check
and does not establish product runtime acceptance.

The coordinator owns full `make check`, independent executed review, test QA,
required published-head CI and PR publication. No full application/race run,
standalone build, install, runtime activation or deployment was performed here.
T030/SC-018 remain open for the wider safety matrix and historical evidence
limits in the [core record](t030-init-core-20261024.md). No task/progress checkbox,
CI inventory, contract or specification was changed.
