---
title: External skill split runbook record
description: "Sanitized historical extraction and integration instructions from issue 283."
weight: 14
toc: true
---

This is a sanitized archival copy of [#283](https://github.com/wstein/workharbor/issues/283)'s
original extraction and integration instructions, preserved before issue
consolidation. It is historical evidence, not a new executable tool or current
instruction to repeat the import. The import phase is complete at
`1c784080bc0dee2060066aaf2dbc8f3894dc430d` from source
`c6bbb7bcd903ea3027285baa9237f4ad179a9bb7`; historical empty-repository,
Todo/ownership and pre-import statements below describe that earlier phase.
Current contracts and implementation routing are in
[external skill sets](../design/skill-sets.md),
[native Codex supervision](../design/native-codex.md),
[canonical native role bindings](../design/native-bindings.md) and
[the skill-set contract](skill-set-contract.md). Their recorded decisions
supersede unresolved design details in this historical text. Check current
issue state before acting; this page grants no push or deletion permission.

Sanitization replaces the machine-local tool directory with the portable
`extraction-tools` directory relative to a dedicated disposable workspace,
and uses the system temporary-directory selection instead of a machine path.
Create that directory in the disposable workspace and run the archived
sequence from that workspace if reproduction is separately authorized.
The scripts and handoff paths below are preserved as text, not installed tools.
Code indentation is normalized without changing block structure.
These substitutions have not been rerun; the original reported measurements
apply to the original pipeline. This is not raw output or a public bundle.
Original security checks, source/file boundaries, commands, extraction identity,
verification results and remaining acceptance conditions are retained.

Preservation mapping: original detailed instructions, acceptance criteria,
method references, CLI runbook A–F and runtime-loading acceptance appear below.
The later stage-1 implementation summary remains in the issue and its named
commits; it is not replaced by this historical runbook. Pending policy-source
proposals are preserved separately in [Pending policy proposal](pending-policy-proposal.md).

## Detailed implementation instructions

### 1. Inventory and agree the boundary
- Record a source commit on main approved for the extraction and an exact file manifest. Check for ongoing lane changes and choose a coordinated cutover point.
- Audit dependencies on `AGENTS.md`, relative links, worktree names, board identifiers, repository URLs, Make targets, permissions, model names and Go checks.
- Inspect historical names in an isolated clone; path filtering does not automatically follow renames. Include former paths where needed.
- Separate generic team coordination from WorkHarbor's local contribution/security policy. One source owns each rule. Preserve every existing security control unless Werner explicitly approves a change.
- Route changes to decision table/rule sections/threat model through wh/design. Read the design history before proposing those edits.

### 2. Extract history in a disposable clone
- Use a fresh dedicated clone, never the shared checkout or an active lane worktree. Use the shared isolated git environment/credential controls; do not access the human's keychain or credential stores.
- Filter the agreed directories/files with repeated `--path` arguments (and agreed historical paths); use path renames only where the destination layout requires them. Never use `--invert-paths` on the original repository, bypass the fresh-clone check with `--force`, or force-push WorkHarbor.
- Select main as the initial lineage; do not import unrelated product branches or release tags into crewbook without a documented reason.
- Preserve relevant authorship, messages and history. Extracted commit IDs will change. Retain the filter-repo commit map and record the source repository, source commit, manifest and reproducible extraction command as provenance.
- Scan the extracted tree, retained history and commit messages for secrets and personal/machine data before publication. Do not print matches; stop and report a finding. Preserve the existing EUPL-1.2 licence and attribution; do not infer a licence change.
- Keep the GitHub repository empty until the verified extraction is ready: no automatic README/licence/gitignore commit. Publication/push requires an explicit request for that push; creating the repository does not authorize pushing extracted content.

### 3. Make crewbook portable
- Add a short README, install/update/uninstall instructions, role documentation and a real skill entry point (`SKILL.md`) if distributing as a skill. Read the applicable skill-creator instructions before implementing that package.
- Use explicit project configuration for repository identity, board, lane/worktree names, check/land commands and local policies. Ship a WorkHarbor example without personal absolute paths or credentials.
- Keep tool-specific entry points thin and resolve links against the installed layout. Define how project-specific `AGENTS.md` is loaded and how conflicting instructions are handled without weakening local hard rules.
- Record Werner's authorized Codex model mapping: Sonnet roles -> `gpt-6.1-sol` with low reasoning; Opus roles -> `gpt-6.1-sol` with medium reasoning; Haiku roles -> `gpt-6-luna` with medium reasoning. Set model and effort explicitly. Document tool-specific mappings separately rather than claiming those model IDs work in Claude Code.
- Establish the integration contract: required commands, supported tool/version assumptions, installation location, local overrides and compatibility/version checking.

### 4. Integrate and cut over WorkHarbor
- Choose an explicit immutable crewbook commit pin. Decide installation/vendoring separately from extraction; subtree vendoring is an option, not a requirement. Never load an unreviewed latest version automatically.
- Preserve a working invocation path for all lanes and commands. Update relative links, manual pages and generated documentation where applicable.
- Resolve `internal/doctor/laneagents.go`, which discovers `.claude/agents/wh-*.md`, and `internal/docscheck/agents_test.go` and related checks. Keep detection and missing-permission diagnostics valid for the chosen installation layout; do not silence checks to make the split pass.
- Retain project-specific rules and permissions in WorkHarbor. Remove duplicated reusable sources only after the pinned integration works, through a normal reviewed commit, keeping original history intact.
- Define rollback to the pre-cutover source commit/pinned version. Existing sessions must be able to finish or explicitly restart using the agreed version.

### 5. Validate and hand over
- Verify the extracted manifest, relevant file history, provenance, licence, secret scans, links and install/update/uninstall behaviour.
- Exercise the WorkHarbor workflow and a second minimal project example to demonstrate portability; do not claim unmeasured behaviour as verified.
- Add focused meaningful tests for changed discovery/configuration/policy behaviour. Run `make check-local` and focused checks before each WorkHarbor commit; record scope, outcomes and exact candidate SHA for independent review. Full local suites require an explicit human request before push. Never bypass hooks.
- Security-relevant changes receive independent wh/review at the authorized Opus-equivalent model strength (`gpt-6.1-sol`, medium). Authors never review their own changes. Rule changes remain wh/design's responsibility.
- Werner controls publication, pushing, release and final cutover. Desk prepares and routes; dispatch starts workers only when routed. Board writes use `scripts/board-snapshot.sh` exclusively.

## Design references and dependencies

The expanded scope also concerns architecture §5.2 (agent adapter/settings), §5.5 (adapter plugins) and the distinction between skill-set provisioning and untrusted repository-discovered skills. A skill-set package is not automatically an executable adapter plugin.

- `docs/content/docs/design/decisions.md`, §3: D34 (dogfooding), D42 (workspaces/roles); any changed decision belongs to wh/design.
- `docs/content/docs/design/architecture.md`, §5.2: agent adapter and supervisor-controlled settings. Extracting development skills must not enable untrusted repository skills in supervised runs or weaken approval/permission controls.
- `docs/content/docs/design/interfaces.md`: lane-agents doctor behaviour.
- `docs/content/docs/manual/sessions-and-agents.md`: lane lifecycle, models, review and handover.
- `AGENTS.md` and `.agents/desk.md`: project policy, ownership, board/REST workflow and desk limits.
- Dependencies: approved boundary/configuration contract, verified extraction, reviewed pinned integration and Werner's explicit publication/cutover instructions. No guessed issue dependencies.

## Acceptance criteria

- [x] Empty `wstein/crewbook` repository prepared with the agreed description and topics.
- [x] Work item assigned to `wstein`; project Status `Todo`, Priority `P1`, Session `Werner`.
- [ ] Source commit, extraction manifest, historical paths and ownership boundary recorded.
- [ ] Cleanup removes only files/content identified in the approved extraction manifest; broader removal has a separately explicit file boundary from Werner.
- [ ] Relevant workflow history extracted with filter-repo in an isolated clone; WorkHarbor history unchanged.
- [ ] EUPL-1.2 licence, attribution, provenance and commit map retained; tree/history/message scans pass.
- [ ] Reusable workflow and tool entry points packaged with configuration and documented model mappings.
- [ ] Installation/update/uninstall, immutable pin and rollback documented and exercised.
- [ ] WorkHarbor consumes the pin; lane discovery, links, local policies and permissions remain functional.
- [ ] A second project example demonstrates reuse; unmeasured claims are marked unverified.
- [ ] Required checks and independent review pass; wh/design resolves any rule changes.
- [ ] Raw crewbook import published (complete); remaining WorkHarbor integration/cutover checks, review and final handover complete. Further pushes remain separately authorized.

## Method references

- https://github.com/newren/git-filter-repo/blob/main/Documentation/git-filter-repo.txt
- https://github.com/git/git/blob/master/contrib/subtree/git-subtree.adoc


## CLI runbook and required files

This is a runbook for Werner/the assigned implementation lane, not a record of commands already executed. Replace placeholders only after the extraction manifest and integration contract are approved. Never run extraction commands in the shared checkout, a live lane worktree or the dirty design worktree. No force push, original-history rewrite, branch switching in the shared checkout or credential-store access. Agents still need explicit authorization for each push. Human-only publication commands below are not an agent authorization.

### A. Inspect GitHub setup (already prepared)

Use existing authorized authentication. Never call `gh auth`, `git credential`, macOS `security`, or put a token on the command line. If an agent needs credentials, provision them through the approved 0600 env-file mechanism; do not print their contents.

```sh
gh api repos/wstein/crewbook --jq '{full_name,description,private,topics,size}'
gh api repos/wstein/workharbor/issues/283 --jq '{number,title,assignees:[.assignees[].login],body}'
scripts/board-snapshot.sh card 283
```

Do not create the repository again. `wstein/crewbook` already exists, public and empty, with description and topics. Keep it empty until import; no auto-initialized README/licence commit. Issue #283 is assigned to wstein, Todo/P1/Session Werner. Board changes go only through `scripts/board-snapshot.sh` with authorization.

### B–D. Corrected, tested fail-fast extraction pipeline

The previous manual block failed when `extraction-paths.txt` had not been created; the shell then continued against the unfiltered product repository. **Do not publish any bundle from that failed attempt.** A successful fsck/bundle verify alone does not prove extraction.

The following four scripts were executed in sequence to create the verified review bundle. They generate explicit manifests, include the audited historical `.agents/worker.md` path, verify all retained history trees against the allowlist, compare all tip blobs to source, scan retained history/tree/messages, and clone the final bundle with `--branch main`. Each subprocess uses `check=True` or explicit scan-result gates, and the shell stops on any script failure. No bundle is produced before filter/content/security verification.

Source for this review: `c6bbb7bcd903ea3027285baa9237f4ad179a9bb7`, matching Werner's failed attempt, not automatically the latest main. This is a **raw extraction for review**, not completed crewbook packaging/integration. The removal manifest is a candidate only: it excludes LICENSE and the mixed team manual; actual cleanup requires review and successful integration. The whole manual is retained for history, then split by sections in an ordinary later commit. No original files/history changed and nothing was pushed.

Tools needed: Python 3, Git, the installed reviewed git-filter-repo, Go for the source-pinned gitleaks scans. Public-source clone has no credentials, ignores global/system Git configuration and disables hooks/fsmonitor/SSH agent. No HOME reassignment, keychain access or force option. Git operations take place only in newly created disposable directories. Source-pinned gitleaks module: `github.com/zricethezav/gitleaks/v8@v8.30.1`.

To reproduce, save the following scripts at the shown paths (the paths are a dedicated temporary tool directory, not a work repository), then run the final bash command. Reproduction creates a new isolated clone each time. Never run just the later bundling step after a failed earlier step.

```sh
mkdir -p extraction-tools
```

<details>
<summary>extraction-tools/prepare.py</summary>

```python
import json, os, pathlib, subprocess, tempfile
root=pathlib.Path(tempfile.mkdtemp(prefix='crewbook-verified-'))
(root/'evidence').mkdir()
env={'PATH':os.environ['PATH'],'LANG':'C','GIT_CONFIG_SYSTEM':'/dev/null','GIT_CONFIG_GLOBAL':'/dev/null','GIT_CONFIG_NOSYSTEM':'1','GIT_TERMINAL_PROMPT':'0','GIT_ASKPASS':'/usr/bin/true','SSH_ASKPASS':'/usr/bin/true','SSH_AUTH_SOCK':''}
pairs=[('credential.helper',''),('core.hooksPath','/dev/null'),('core.fsmonitor','false'),('protocol.ext.allow','never'),('user.useConfigOnly','true')]
env['GIT_CONFIG_COUNT']=str(len(pairs))
for n,(k,v) in enumerate(pairs):env[f'GIT_CONFIG_KEY_{n}']=k;env[f'GIT_CONFIG_VALUE_{n}']=v
(root/'evidence/git-environment.json').write_text(json.dumps(env))
def git(*args,cwd=None):return subprocess.run(['git',*args],cwd=cwd,env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE).stdout
try:
  git('clone','--single-branch','--branch','main','--no-tags','https://github.com/wstein/workharbor.git',str(root/'source'))
  sha=git('rev-parse','c6bbb7b^{commit}',cwd=root/'source').decode().strip()
  assert not git('status','--porcelain',cwd=root/'source')
  git('reset','--hard',sha,cwd=root/'source')
  git('clone','--no-local','--single-branch','--branch','main','--no-tags',str(root/'source'),str(root/'import'))
  (root/'evidence/source-commit.txt').write_text(sha+'\n')
  (root/'evidence/source-files.txt').write_bytes(git('ls-tree','-r','--name-only',sha,cwd=root/'source'))
  paths=['LICENSE',*[f'.agents/{n}.md' for n in ['code','design','desk','dispatch','docs','helper','review','verify']],*[f'.claude/agents/{n}.md' for n in ['wh-platform','wh-runtime','wh-docs','wh-verify','wh-worker','wh-design','wh-reviewer','wh-docs-reviewer','wh-helper','wh-helper-edit']],*[f'.claude/commands/{n}.md' for n in ['wh-code','wh-design','wh-desk','wh-dispatch','wh-docs','wh-review','wh-verify','wh-delegate','wh-board','wh-handover','wh-land']],'docs/content/docs/manual/sessions-and-agents.md']
  sourcefiles=set((root/'evidence/source-files.txt').read_text().splitlines())
  assert set(paths)<=sourcefiles, 'candidate file absent at source commit'
  hashes={p:git('rev-parse',f'{sha}:{p}',cwd=root/'source').decode().strip() for p in paths}
  (root/'evidence/source-blobs.json').write_text(json.dumps(hashes,indent=2))
  (root/'evidence/current-paths.txt').write_text('\n'.join(paths)+'\n')
  (root/'evidence/extraction-paths.txt').write_text('\n'.join(paths)+'\n')
  (root/'evidence/removal-manifest.txt').write_text('\n'.join(p for p in paths if p!='LICENSE' and p!='docs/content/docs/manual/sessions-and-agents.md')+'\n')
  history=[]
  for p in paths:
    history.append('PATH '+p+'\n'+git('log','--follow','--format=','--name-status','--',p,cwd=root/'source').decode())
  (root/'evidence/path-history.txt').write_text('\n'.join(history))
  for name in ['Makefile','.gitleaks.toml']:(root/'evidence'/('source-'+name.lstrip('.'))).write_bytes(git('show',f'{sha}:{name}',cwd=root/'source'))
  (root/'evidence/filter-repo-version.txt').write_bytes(git('filter-repo','--version',cwd=root/'import'))
  pathlib.Path('extraction-tools/latest-root.txt').write_text(str(root))
  print(json.dumps({'root':str(root),'source_commit':sha,'current_files':len(paths)}))
except subprocess.CalledProcessError as e:
  print('Command failed, extraction stopped. '+e.stderr.decode(errors='replace')[:2000]);raise

```

</details>

<details>
<summary>extraction-tools/filter.py</summary>

```python
import hashlib,json,pathlib,subprocess
root=pathlib.Path(pathlib.Path('extraction-tools/latest-root.txt').read_text())
e=root/'evidence'; env=json.loads((e/'git-environment.json').read_text()); repo=root/'import'
def git(*args):return subprocess.run(['git',*args],cwd=repo,env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE).stdout
paths=(e/'extraction-paths.txt').read_text().splitlines()
paths.append('.agents/worker.md')
(e/'historical-paths.txt').write_text('.agents/worker.md\n')
(e/'extraction-paths.txt').write_text('\n'.join(paths)+'\n')
assert (e/'extraction-paths.txt').is_file() and not (e/'extraction-paths.txt').is_symlink()
assert not git('status','--porcelain')
assert git('rev-parse','HEAD').decode().strip()==(e/'source-commit.txt').read_text().strip()
assert not (repo/'.git/filter-repo').exists(),'fresh import required'
try:
  out=git('filter-repo','--paths-from-file',str(e/'extraction-paths.txt'))
  (e/'filter-output.txt').write_bytes(out)
  (e/'fsck.txt').write_bytes(git('fsck','--full'))
  actual=set(git('ls-tree','-r','--name-only','HEAD').decode().splitlines())
  expected=set((e/'current-paths.txt').read_text().splitlines())
  assert actual==expected, f'tree mismatch: extra={actual-expected}, missing={expected-actual}'
  blobs=json.loads((e/'source-blobs.json').read_text())
  for p,h in blobs.items():assert git('rev-parse',f'HEAD:{p}').decode().strip()==h, 'changed source blob: '+p
  commits=git('rev-list','main').decode().splitlines()
  seen=set()
  for commit in commits:
    names=set(git('ls-tree','-r','--name-only',commit).decode().splitlines())
    assert names<=set(paths), 'unexpected historical content'
    seen.update(names)
  (e/'import-files.txt').write_text('\n'.join(sorted(actual))+'\n')
  (e/'retained-history-paths.txt').write_text('\n'.join(sorted(seen))+'\n')
  (e/'commit-map.txt').write_bytes((repo/'.git/filter-repo/commit-map').read_bytes())
  (e/'import-log.txt').write_bytes(git('log','--format=fuller','main'))
  (e/'commit-messages.txt').write_bytes(git('log','--all','--format=%B'))
  result={'source_commit':(e/'source-commit.txt').read_text().strip(),'extracted_commit':git('rev-parse','main').decode().strip(),'tip_files':len(actual),'retained_commits':len(commits),'historical_paths':len(seen),'blob_match':True,'all_history_trees_allowlisted':True}
  (e/'verification.json').write_text(json.dumps(result,indent=2)+'\n')
  print(json.dumps(result))
except subprocess.CalledProcessError as err:
  print('Filtering/verification failed; no bundle created. '+err.stderr.decode(errors='replace')[:1000]);raise

```

</details>

<details>
<summary>extraction-tools/scan.py</summary>

```python
import json,os,pathlib,re,subprocess
root=pathlib.Path(pathlib.Path('extraction-tools/latest-root.txt').read_text()); e=root/'evidence'
module=re.search(r'^GITLEAKS := (\S+@\S+)$',(e/'source-Makefile').read_text(),re.M).group(1)
env=os.environ.copy();env.update(json.loads((e/'git-environment.json').read_text()))
for k in list(env):
  if k in ['SSH_AGENT_PID','GIT_CONFIG_PARAMETERS'] or (k.startswith('GIT_CONFIG_KEY_') or k.startswith('GIT_CONFIG_VALUE_')) and int(k.rsplit('_',1)[1])>=int(env['GIT_CONFIG_COUNT']):env.pop(k,None)
scans=[('history',['git','--log-opts=--all','.'],None),('tree',['dir','.'],None),('messages',['stdin'],(e/'commit-messages.txt').read_bytes())]
results=[]
for name,args,stdin in scans:
  cmd=['go','run',module,args[0],'--no-banner','--redact','--config',str(e/'source-gitleaks.toml'),*args[1:]]
  p=subprocess.run(cmd,cwd=root/'import',env=env,input=stdin,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  log=p.stdout+p.stderr;(e/f'scan-{name}.log').write_bytes(log);os.chmod(e/f'scan-{name}.log',0o600)
  plain=re.sub(rb'\x1b\[[0-9;]*m',b'',log)
  ok=p.returncode==0 and b'no leaks found' in plain
  result={'scan':name,'exit_code':p.returncode,'no_leaks_found':ok,'module':module}
  results.append(result)
  print(json.dumps(result),flush=True)
  if not ok:
    (e/'scan-results.json').write_text(json.dumps(results,indent=2))
    raise SystemExit('Scan did not pass. No bundle will be published; diagnostic log withheld pending inspection.')
(e/'scan-results.json').write_text(json.dumps(results,indent=2)+'\n')

```

</details>

<details>
<summary>extraction-tools/bundle.py</summary>

```python
import hashlib,json,pathlib,shutil,subprocess
root=pathlib.Path(pathlib.Path('extraction-tools/latest-root.txt').read_text());e=root/'evidence';env=json.loads((e/'git-environment.json').read_text());out=root/'review';out.mkdir(exist_ok=True)
def git(*args,cwd=None):return subprocess.run(['git',*args],cwd=cwd or root/'import',env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
assert all(r['no_leaks_found'] and r['exit_code']==0 for r in json.loads((e/'scan-results.json').read_text()))
v=json.loads((e/'verification.json').read_text());bundle=out/'crewbook-import.bundle'
git('bundle','create',str(bundle),'main')
r=git('bundle','verify',str(bundle));(out/'bundle-verify.txt').write_bytes(r.stdout+r.stderr)
git('clone','--no-local','--branch','main',str(bundle),str(root/'bundle-check'))
assert git('rev-parse','main',cwd=root/'bundle-check').stdout.decode().strip()==v['extracted_commit']
assert git('ls-tree','-r','--name-only','main',cwd=root/'bundle-check').stdout==git('ls-tree','-r','--name-only','main').stdout
git('fsck','--full',cwd=root/'bundle-check')
for n in ['source-commit.txt','extraction-paths.txt','current-paths.txt','historical-paths.txt','removal-manifest.txt','source-blobs.json','import-files.txt','retained-history-paths.txt','commit-map.txt','verification.json','scan-results.json','filter-repo-version.txt']:
  shutil.copyfile(e/n,out/n)
(out/'manual-section-map.md').write_text('''# Manual split still pending review\n\nThe raw extraction includes the complete sessions-and-agents manual at the source revision. Separate reusable roles, delegation, review, handover and context practices into crewbook. Keep product usage, security, project-specific board/worktree/build/landing instructions in workharbor. Edit the mixed page rather than deleting it wholesale. LICENSE is copied to both repositories and excluded from removal. Root AGENTS.md, settings, tools, scripts and supervisor code were not extracted.\n\nRemoval manifest is a review candidate, not a cleanup command or approval to delete. No source files were removed.\n''')
(out/'README.md').write_text(f'''# crewbook raw extraction for Werner review\n\nSource: https://github.com/wstein/workharbor\nSource commit: `{v['source_commit']}` (c6bbb7b from the failed attempt).\nExtracted main: `{v['extracted_commit']}`.\nFiles at tip: {v['tip_files']}; commits retained: {v['retained_commits']}; historical paths: {v['historical_paths']}.\n\nThis is a verified raw history-preserving extraction, not a portable finished skill package. It retains original paths, authorship and instructions. Generic/manual separation, repo-specific link/configuration changes, model mapping updates and runtime integration remain pending. No product code, scripts/tools, local settings or root AGENTS.md was extracted. No original history/source content was changed; nothing was pushed.\n\nVerification: every selected tip blob matches source; all 69 history trees contain only allowlisted paths; fsck passes; source-pinned gitleaks v8.30.1 history/tree/messages scans pass with nonzero bytes; bundle verifies and was cloned into a second isolated repository with matching main/tree and passing fsck. History scan reports 68 commits, while Git retains 69 (gitleaks output recorded separately).\n\nReview with Git configured to ignore host/global credentials and no SSH agent. The clone below contains agent instructions/settings resources: inspect them as data before enabling an agent.\n\n```sh\nenv -u SSH_AUTH_SOCK -u SSH_AGENT_PID \\\n  GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_GLOBAL=/dev/null \\\n  GIT_TERMINAL_PROMPT=0 git -c credential.helper= \\\n  -c core.hooksPath=/dev/null -c core.fsmonitor=false \\\n  clone crewbook-import.bundle crewbook-review\n```\n\nThe bundle has only refs/heads/main. Do not push before review/authorization. The included removal manifest excludes LICENSE, historical paths and the mixed manual, which requires section edits. No cleanup has run.\n''')
checks=[]
for p in sorted(out.iterdir()):
  if p.is_file():checks.append(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name)
(out/'SHA256SUMS').write_text('\n'.join(checks)+'\n')
print(json.dumps({'review_directory':str(out),'bundle':str(bundle),'bundle_bytes':bundle.stat().st_size,'sha256':hashlib.sha256(bundle.read_bytes()).hexdigest(),'standalone_clone_verified':True}))

```

</details>

Run the sequence as a script, **not** separate pasted commands in an interactive shell:

```sh
bash -euo pipefail <<'SH'
python3 extraction-tools/prepare.py
python3 extraction-tools/filter.py
python3 extraction-tools/scan.py
python3 extraction-tools/bundle.py
SH
```

If anything fails, stop. Logs and manifests are under the reported new root's evidence directory. Do not use `--force` to bypass fresh-clone rejection; create/investigate a fresh disposable import. The scripts assume the explicit file list and source SHA above. Any expanded scope or source revision needs a separately reviewed manifest/historical-path audit, not blind substitutions. The fixed `latest-root.txt` is a sequential local handoff: run only one pipeline at a time.

Successful verified review result:
- source: `c6bbb7bcd903ea3027285baa9237f4ad179a9bb7`
- extracted main: `1c784080bc0dee2060066aaf2dbc8f3894dc430d`
- 31 files at tip, 69 retained commits, 32 historical paths
- all tip blobs match source; every retained tree is allowlisted
- history/tree/messages gitleaks scans passed with nonzero bytes (history scanner reports 68 scanned commits while Git retains 69)
- bundle verified; a second isolated clone using `--branch main` matches the extracted main/tree and passes fsck
- bundle size: 119,773 bytes
- bundle SHA256: `689df4a1aa4aed86532548a5549ec6fba1507c40f7b55301cab1074f83c5544f`

Review files supplied locally to Werner. A machine-local path is not a public attachment; no bundle was uploaded by this issue edit. Further portability/manual edits, model updates, runtime skill loading and cutover remain pending. Do not claim this raw bundle completes #283.

### E. Complete the crewbook package and publish the approved import

Required crewbook files/artifacts, created in ordinary reviewed commits after raw extraction:

- `README.md`: purpose, relationship to WorkHarbor and package scope.
- `LICENSE`: EUPL-1.2 and preserved attribution.
- A real `SKILL.md` entry point and referenced role/manual resources; consult skill-creator instructions first. Tool-specific launchers must use the installed resource layout.
- Generic team operating manual and a documented configuration example, without personal absolute paths, secrets or assumed WorkHarbor project identity.
- A documented package/layout/compatibility contract and manifest covering skill-set identity/version, resources and checksums as agreed by wh/design. Do not invent unsupported platform configuration keys.
- Install/select/pin/update/uninstall and rollback instructions; explicit model mappings and instruction precedence.
- `PROVENANCE.md` (or equivalent) with source SHA, selection/section manifests, extraction command/tool version and verification summary.
- Crewbook-appropriate validation/CI, contribution guidance and secret scanning; do not inherit WorkHarbor supervisor checks blindly or bypass its imported instructions. Executable tools remain workharbor-side.

After required checks/review and **Werner's explicit authorization for this push**, import main only. Human-only command outline, with human-managed authentication; do not use the credential-disabled extraction wrapper as an authenticated agent push workaround:

```sh
# In the verified crewbook import clone, operated by Werner:
git remote -v
# filter-repo normally removes origin. Add the new remote only if absent:
git remote add origin https://github.com/wstein/crewbook.git
git push -u origin main
# No --mirror, --force or product release tags.

gh api repos/wstein/crewbook/commits/main --jq '.sha'
gh api repos/wstein/crewbook --jq '{default_branch,description,topics}'
```

Stop if origin already exists or the destination has unexpected commits; inspect rather than overwrite. Repository creation in this session does not authorize this content push. Branch protection/release settings require an explicit choice; do not guess account-plan capabilities.

### F. Integrate and remove only approved sources

Use the appropriate clean lane worktree/new branch and normal issue lifecycle after Werner routes implementation. Never use the shared checkout for edits/git operations. Inventory at least:

- `AGENTS.md` and remaining repo-local tool settings/wrappers.
- `internal/doctor/laneagents.go`, `laneagents_test.go` and shared doctor registration/configuration.
- `internal/docscheck/agents_test.go` and other docs/link/generated checks.
- `docs/content/docs/manual/sessions-and-agents.md` and callers/links found by rg.
- `docs/content/docs/design/interfaces.md`, `architecture.md` and any actual affected decisions/rules (owned by wh/design).
- The runtime/agent configuration and provisioning/mount paths affected by external skill sets; start/resume behaviour and run-version provenance. A mount alone is not proof skills are loaded.
- `.claude/settings.json`, board/landing script dependencies and Make targets as retained project adapters, changing only what the agreed contract requires.

```sh
# Read-only dependency audit; run from the implementation worktree:
rg -n --hidden -g '!.git' \
  '\.agents|\.claude/agents|\.claude/commands|sessions-and-agents|lane-agents' .
# Review removals from removal-manifest.txt one file at a time.
# Do not run rm -rf .agents/.claude/docs or git rm on an entire guessed directory.
make hooks
make check-local
# Run focused behaviour/regression checks for this slice and record evidence.
make commitlint
```

Implement external skill-set storage and the agreed runtime container mount, with an immutable crewbook pin as the default and supported alternatives. Use the runtime adapter's actual capabilities, not assumed Docker flags. Prefer read-only skill content under a dedicated mount path outside the agent-writable repository; wh/design finalizes the contract. Never mount host home, credentials or runtime sockets. Retain supervisor-controlled settings, approval enforcement and repository-skill/hook/MCP restrictions on start and resume.

Record what runtime loading is actually delivered and test it; configuration/docs-only integration must not be described as working runtime provisioning. If runtime implementation exceeds this issue's agreed scope, split it into an explicitly linked follow-up before claiming this acceptance criterion complete. Further crewbook development remains separate work.

Run focused behavioural/integration checks, independent review and the required WorkHarbor checks. Remove only the approved current-source files after import verification and integration tests; edit mixed manual pages according to the section map. Use normal reviewed WorkHarbor commits and the authorized landing process; never rewrite product history or bypass hooks. Update acceptance criteria and final provenance in #283 through REST. All pushes remain Werner-controlled.


## Runtime-loading integration acceptance (desk recommendation)

In response to Werner's question about convenience, desk recommends including **working runtime skill loading** in #283's integration deliverable rather than stopping at mounting/configuration/documentation. This is a narrowly scoped integration recommendation for wh/design to finalize against the agreed platform security contract.

- [ ] A run actually receives the selected, immutable skill-set content through supervisor-controlled provisioning outside the agent-writable repository; crewbook is the configured default.
- [ ] A new run can use an explicitly selected compatible alternative without a platform code change.
- [ ] Start and resume use the recorded skill-set revision, or explicitly refuse an incompatible/unavailable revision; an update does not silently change an existing run.
- [ ] Integration evidence demonstrates instruction loading, not merely mount existence. Existing repository settings/skill/hook/MCP restrictions and approval enforcement remain intact.
- [ ] Missing/incompatible skill sets produce a clear error or the explicitly agreed fallback; no unreviewed latest download or silently ignored loading.

Keep this limited to extraction/import/cleanup and the minimal runtime integration required to consume the package. No new crewbook workflows or generic executable-tool/plugin system are included. If the working-runtime deliverable cannot be achieved under this issue's approved contract/scope, record an explicit linked follow-up and leave its acceptance unticked.
