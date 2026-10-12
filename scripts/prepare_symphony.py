#!/usr/bin/env python3
"""Render non-secret operator settings; no daemon, tracker mutation or network."""
import json
from dataclasses import dataclass
import hashlib
import os
import re
import shutil
import shlex
import subprocess
import sys
import stat
from types import MappingProxyType
from pathlib import Path

SYMPHONY_REVISION = 'be10a1b79df723d6d7612b5651c8522704dafb2e'


def _digest(data):
    return hashlib.sha256(data).hexdigest()


def nonsecret_configuration(config):
    """Project the pinned workflow's explicit settings; never serialize credentials.

    Hook bytes are bound by digests, and the tracker token must remain the exact
    environment reference. Unknown fields block rather than disappear from the
    effective snapshot. This observes settings, not native sandbox enforcement.
    """
    from task_package import PackageError, canonical_json, require_keys
    try:
        require_keys(config, {'tracker', 'polling', 'workspace', 'hooks', 'agent', 'codex'})
        tracker = require_keys(config['tracker'], {'kind', 'provider', 'required_labels',
                                                   'active_states', 'terminal_states'})
        provider = require_keys(tracker['provider'], {'repo', 'token'})
        polling = require_keys(config['polling'], {'interval_ms'})
        workspace = require_keys(config['workspace'], {'root'})
        hooks = require_keys(config['hooks'], {'after_create', 'before_run', 'timeout_ms'})
        agent = require_keys(config['agent'], {'max_concurrent_agents', 'max_turns', 'max_retry_backoff_ms'})
        codex = require_keys(config['codex'], {'command', 'approval_policy', 'thread_sandbox',
                                              'turn_sandbox_policy', 'read_timeout_ms', 'stall_timeout_ms'})
        granular = require_keys(require_keys(codex['approval_policy'], {'granular'})['granular'],
                               {'sandbox_approval', 'rules', 'mcp_elicitations',
                                'request_permissions', 'skill_approval'})
        sandbox = require_keys(codex['turn_sandbox_policy'], {'type', 'networkAccess'})
        if (tracker['kind'] != 'github' or provider['token'] != '$GITHUB_TOKEN'
                or type(provider['repo']) is not str
                or not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', provider['repo'])
                or type(workspace['root']) is not str or not Path(workspace['root']).is_absolute()
                or codex['command'] != 'codex app-server'
                or codex['thread_sandbox'] not in ('read-only', 'workspace-write')
                or sandbox['type'] != 'workspaceWrite' or type(sandbox['networkAccess']) is not bool
                or any(type(item) is not bool for item in granular.values())):
            raise ValueError
        for names in (tracker['required_labels'], tracker['active_states'], tracker['terminal_states']):
            if (type(names) is not list or not names or len(names) > 100
                    or any(type(name) is not str or not re.fullmatch(r'[A-Za-z0-9_-]{1,64}', name)
                           for name in names) or len(set(names)) != len(names)):
                raise ValueError
        integers = [polling['interval_ms'], hooks['timeout_ms'], *agent.values(),
                    codex['read_timeout_ms'], codex['stall_timeout_ms']]
        if any(type(value) is not int or not 0 < value <= 2 ** 31 - 1 for value in integers):
            raise ValueError
        if any(type(hooks[key]) is not str for key in ('after_create', 'before_run')):
            raise ValueError
        result = dict(repository=provider['repo'], workspace_root=workspace['root'],
                      required_labels=tracker['required_labels'], active_states=tracker['active_states'],
                      terminal_states=tracker['terminal_states'], polling_interval_ms=polling['interval_ms'],
                      after_create_digest=_digest(hooks['after_create'].encode('utf-8')),
                      before_run_digest=_digest(hooks['before_run'].encode('utf-8')),
                      hook_timeout_ms=hooks['timeout_ms'], **agent,
                      codex_command=codex['command'], approval_policy=codex['approval_policy'],
                      thread_sandbox=codex['thread_sandbox'], turn_sandbox_policy=sandbox,
                      read_timeout_ms=codex['read_timeout_ms'], stall_timeout_ms=codex['stall_timeout_ms'])
        # Return detached JSON, never references to mutable host configuration.
        return json.loads(canonical_json(result))
    except (KeyError, TypeError, ValueError, UnicodeError):
        raise PackageError('runtime-configuration-invalid') from None


@dataclass(frozen=True)
class RuntimeInputs:
    """Trusted host-selected local paths/settings; package text selects no adapter.

    Build metadata schema: schema_version=1, symphony_revision, source_tree,
    artifact_path, artifact_sha256. Its digest binds the approval, while the
    source checkout and artifact bytes are independently read. Metadata alone
    does not authenticate who built or launched an artifact.
    """
    source_root: Path | None = None
    artifact_root: Path | None = None
    build_metadata_path: str = 'build.json'
    workflow_root: Path | None = None
    workflow_path: str = 'WORKFLOW.local.md'
    template_path: str = 'WORKFLOW.md'
    configuration: dict | None = None
    guard_revision: str | None = None
    check_argv: list | None = None
    external_actions: list | None = None


@dataclass(frozen=True)
class RuntimeLiveObservation:
    """Host observation references, bound to this exact local execution snapshot.

    Only trusted host instrumentation may supply this object after observing the
    launched runtime, native isolation and tracker-secret removal from children.
    Local JSON/build manifests and rendered settings are never converted into it.
    References identify durable measured evidence; this library launches nothing.
    """
    binding_digest: str
    runtime_ref: str
    isolation_ref: str
    secret_behavior_ref: str


def _local_file(root, path, *, content=False, git_object=None, git_mode=None):
    """Read/hash bounded regular single-link files with no-follow traversal."""
    import task_package as package
    descriptors = []
    limit = package.MAX_JSON_BYTES if content else 256 * 1024 * 1024
    try:
        parts = package._path_parts(path)
        with package._root_directory(root) as (_root, parent, links):
            for name in parts[:-1]:
                initial = os.stat(name, dir_fd=parent, follow_symlinks=False)
                child = package.state._directory(parent, name)
                descriptors.append(child)
                links.append((parent, name, child, initial))
                parent = child
            name = parts[-1]
            initial = os.stat(name, dir_fd=parent, follow_symlinks=False)
            if (not stat.S_ISREG(initial.st_mode) or initial.st_nlink != 1
                    or initial.st_size > limit
                    or git_mode is not None and bool(initial.st_mode & 0o111) != (git_mode == b'100755')):
                raise package.PackageError('runtime-file-unsafe')
            descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC,
                                 dir_fd=parent)
            descriptors.append(descriptor)
            if package._file_identity(initial) != package._file_identity(os.fstat(descriptor)):
                raise package.PackageError('runtime-input-raced')
            digest = hashlib.sha1() if git_object is not None and len(git_object) == 40 else hashlib.sha256()
            if git_object is not None:
                digest.update(b'blob ' + str(initial.st_size).encode('ascii') + b'\0')
            chunks, size = [], 0
            while True:
                chunk = os.read(descriptor, min(65536, limit + 1 - size))
                if not chunk:
                    break
                size += len(chunk)
                if size > limit:
                    raise package.PackageError('runtime-file-limit')
                digest.update(chunk)
                if content:
                    chunks.append(chunk)
            if (size != initial.st_size
                    or package._file_identity(initial) != package._file_identity(os.fstat(descriptor))
                    or package._file_identity(initial) != package._file_identity(
                        os.stat(name, dir_fd=parent, follow_symlinks=False))):
                raise package.PackageError('runtime-input-raced')
            package._check_links(links)
            return b''.join(chunks) if content else digest.hexdigest()
    except package.PackageError:
        raise
    except (OSError, TypeError, ValueError):
        raise package.PackageError('runtime-file-unavailable') from None
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def _source(root):
    import task_package as package
    package.resolve_git_object(root, SYMPHONY_REVISION)
    git = lambda *args: package._git(root, *args)
    if git('rev-parse', 'HEAD').strip().decode('ascii') != SYMPHONY_REVISION:
        raise package.PackageError('runtime-source-pin-mismatch')
    # Force mode checks even if local configuration disables them; reject index
    # flags that could hide worktree drift and all extra/ignored source files.
    if (git('-c', 'core.filemode=true', 'status', '--porcelain=v1',
            '--untracked-files=all', '--ignored=matching')
            or any(entry[:2] != b'H ' for entry in git('ls-files', '-v', '-z').split(b'\0') if entry)):
        raise package.PackageError('runtime-source-dirty')
    tree = git('rev-parse', 'HEAD^{tree}').strip().decode('ascii')
    for entry in git('ls-tree', '-r', '-z', SYMPHONY_REVISION).split(b'\0'):
        if not entry:
            continue
        header, path = entry.split(b'\t', 1)
        mode, kind, object_id = header.split(b' ')
        if mode not in (b'100644', b'100755') or kind != b'blob':
            raise package.PackageError('runtime-source-type-unsupported')
        # Git status can omit hardlinks or rely on cached stat data. Read every
        # source blob through the same safe file reader and verify Git identity.
        if _local_file(root, path.decode('utf-8'), git_object=object_id, git_mode=mode) != object_id.decode('ascii'):
            raise package.PackageError('runtime-source-bytes-mismatch')
    return tree


class RuntimeObserver:
    """Read-only RuntimePort. Observations confer no approval or activation.

    observe accepts the approved package so check argv stay owned by its opaque
    guard context. The observer only compares the supplied ordered argv; it does
    not validate TaskContext, call its guard or execute checks. Reobserve at every
    action boundary: this snapshot is neither a lock nor a hostile-code sandbox.
    """
    def __init__(self, inputs=None, *, capabilities=None):
        self.inputs = inputs if inputs is not None else RuntimeInputs()
        self.capabilities = MappingProxyType(dict(capabilities or {}))

    def observe(self, execution):
        return self.observe_effective(execution, {})

    def observe_effective(self, package, evidence, *, live=False):
        import task_package as primitives
        result = dict(schema_version=1, local_verified=False, live_verified=False,
                      execution_eligible=False, activated=False, live_execution=None,
                      execution=None, binding_digest=None, blockers=[])
        try:
            if type(live) is not bool or not isinstance(self.inputs, RuntimeInputs):
                raise primitives.PackageError('runtime-input-invalid')
            inputs = self.inputs
            approved = package['execution']
            if approved['symphony_revision'] != SYMPHONY_REVISION:
                raise primitives.PackageError('runtime-source-pin-mismatch')
            source_tree = _source(inputs.source_root)
            metadata_bytes = _local_file(inputs.artifact_root, inputs.build_metadata_path, content=True)
            metadata = primitives.require_keys(primitives.parse_json(metadata_bytes),
                {'schema_version', 'symphony_revision', 'source_tree', 'artifact_path', 'artifact_sha256'})
            if (type(metadata['schema_version']) is not int or metadata['schema_version'] != 1
                    or metadata['symphony_revision'] != SYMPHONY_REVISION
                    or metadata['source_tree'] != source_tree
                    or not primitives._digest(metadata['artifact_sha256'])):
                raise primitives.PackageError('runtime-build-provenance-mismatch')
            artifact_digest = _local_file(inputs.artifact_root, metadata['artifact_path'])
            if artifact_digest != metadata['artifact_sha256']:
                raise primitives.PackageError('runtime-artifact-mismatch')
            workflow = _local_file(inputs.workflow_root, inputs.workflow_path, content=True)
            template = _local_file(inputs.workflow_root, inputs.template_path, content=True)
            # The pinned runtime recognizes exact standalone delimiter lines,
            # not marker substrings inside JSON. Keep our JSON-only subset and
            # ordinary LF/CRLF/CR line endings compatible with that parser.
            lines = re.split(rb'\r\n|\n|\r', workflow)
            if lines[0] != b'---' or b'---' not in lines[1:]:
                raise primitives.PackageError('runtime-workflow-invalid')
            closing = lines.index(b'---', 1)
            configuration = nonsecret_configuration(
                primitives.parse_json(b'\n'.join(lines[1:closing])))
            # JSON identity compares types too (True must never equal integer 1).
            if primitives.canonical_json(configuration) != primitives.canonical_json(inputs.configuration):
                raise primitives.PackageError('runtime-configuration-mismatch')
            checks = [check['argv'] for check in package['context']['checks']]
            if (primitives.canonical_json(checks) != primitives.canonical_json(inputs.check_argv)
                    or primitives.canonical_json(approved['external_actions']) !=
                    primitives.canonical_json(inputs.external_actions)):
                raise primitives.PackageError('runtime-argv-action-mismatch')
            observed = dict(symphony_revision=SYMPHONY_REVISION, source_tree=source_tree,
                            artifact_digest=artifact_digest, build_provenance=_digest(metadata_bytes),
                            workflow_digest=_digest(workflow), template_digest=_digest(template),
                            configuration_digest=_digest(primitives.canonical_json(configuration)),
                            guard_revision=inputs.guard_revision,
                            argv_digest=_digest(primitives.canonical_json(checks)),
                            actions_digest=_digest(primitives.canonical_json(inputs.external_actions)))
            if not primitives._revision(inputs.guard_revision):
                raise primitives.PackageError('runtime-guard-unavailable')
            for key in ('symphony_revision', 'build_provenance', 'workflow_digest',
                        'template_digest', 'configuration_digest', 'guard_revision'):
                if observed[key] != approved[key]:
                    raise primitives.PackageError('runtime-execution-drift')
            # A second snapshot rejects changes during collection, without
            # writing/adopting a new baseline or relying on file mtimes alone.
            if (source_tree != _source(inputs.source_root)
                    or metadata_bytes != _local_file(inputs.artifact_root, inputs.build_metadata_path, content=True)
                    or artifact_digest != _local_file(inputs.artifact_root, metadata['artifact_path'])
                    or workflow != _local_file(inputs.workflow_root, inputs.workflow_path, content=True)
                    or template != _local_file(inputs.workflow_root, inputs.template_path, content=True)):
                raise primitives.PackageError('runtime-input-raced')
            result.update(local_verified=True, execution=observed,
                          binding_digest=_digest(primitives.canonical_json(observed)))
            if live:
                observation = evidence.get('runtime') if type(evidence) is dict else None
                if (not isinstance(observation, RuntimeLiveObservation)
                        or observation.binding_digest != result['binding_digest']
                        or any(type(ref) is not str or not re.fullmatch(r'sha256:[0-9a-f]{64}', ref)
                               for ref in (observation.runtime_ref, observation.isolation_ref,
                                           observation.secret_behavior_ref))):
                    raise primitives.PackageError('runtime-live-observation-unavailable')
                # Host capability evidence remains separately required and cannot
                # be manufactured by source/build metadata or a settings render.
                for name in ('runtime.provenance', 'runtime.workflow', 'runtime.configuration',
                             'runtime.host-isolation'):
                    _value, blocker = primitives._capability_evidence(
                        name, self.capabilities.get(name), True)
                    if blocker:
                        raise primitives.PackageError('runtime-live-capability-unavailable')
                result.update(live_verified=True, live_execution=observation.runtime_ref)
        except primitives.PackageError as error:
            result['blockers'].append(dict(code=error.code))
        except Exception:
            # Never echo adapter/input/path/Git diagnostics or environment values.
            result['blockers'].append(dict(code='runtime-observation-unavailable'))
        return result


def render():
    root = Path(__file__).resolve().parents[1]
    required = ('WORKHARBOR_REPO_URL', 'WORKHARBOR_REPO_REVISION', 'WORKHARBOR_WORKSPACE_ROOT', 'WORKHARBOR_GITHUB_REPO', 'GITHUB_TOKEN')
    missing = [key for key in required if not os.environ.get(key)]
    if missing:
        sys.exit('Missing settings: ' + ', '.join(missing) + '; see docs/development/symphony.md')
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', os.environ['WORKHARBOR_GITHUB_REPO']):
        sys.exit('WORKHARBOR_GITHUB_REPO must be owner/repository, not a URL.')
    revision = os.environ['WORKHARBOR_REPO_REVISION']
    if not re.fullmatch(r'[0-9a-f]{40}', revision):
        sys.exit('WORKHARBOR_REPO_REVISION must be a full lowercase commit SHA.')
    feature = os.environ.get('WORKHARBOR_FEATURE')
    if feature and (not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]*', feature)
                    or not (root/'specs'/feature/'spec.md').is_file()):
        sys.exit('WORKHARBOR_FEATURE must name an existing direct specs/ feature.')
    branch = os.environ.get('WORKHARBOR_ISSUE_BRANCH')
    if branch and subprocess.run(['git', 'check-ref-format', '--branch', branch],
                                 capture_output=True).returncode:
        sys.exit('WORKHARBOR_ISSUE_BRANCH must be a valid Git branch.')
    if not shutil.which('codex'):
        sys.exit('Codex CLI missing; install and authenticate it before activation.')
    workspace = Path(os.environ['WORKHARBOR_WORKSPACE_ROOT']).expanduser()
    if not workspace.is_absolute():
        sys.exit('WORKHARBOR_WORKSPACE_ROOT must be absolute.')
    workspace = workspace.resolve()
    if workspace == root or workspace in root.parents or root in workspace.parents:
        sys.exit('Issue workspace root must be separate from this source checkout.')
    text = (root/'WORKFLOW.md').read_text()
    _, front, body = text.split('---', 2)
    config = json.loads(front)
    config['tracker']['provider']['repo'] = os.environ['WORKHARBOR_GITHUB_REPO']
    config['workspace']['root'] = str(workspace)
    hook = ('set -eu\n'
            ': "${WORKHARBOR_REPO_URL:?Set a trusted WorkHarbor repository URL or absolute local path}"\n'
            'git clone --no-local --no-checkout -- "$WORKHARBOR_REPO_URL" .\n')
    if branch:
        hook += f'git switch -c {shlex.quote(branch)} {revision}\n'
    else:
        hook += ('key=$(basename "$PWD")\n'
                 'case "$key" in \'\'|*[!A-Za-z0-9._-]*) exit 1 ;; esac\n'
                 f'git switch -c "work/$key" {revision}\n')
    hook += f'test "$(git rev-parse HEAD)" = {revision}\n'
    if feature:
        hook += f'python3 scripts/select_feature.py {shlex.quote(feature)}\n'
    config['hooks']['after_create'] = hook
    output = root/'WORKFLOW.local.md'
    output.write_text('---\n' + json.dumps(config, indent=2) + '\n---' + body)
    output.chmod(0o600)
    print('Prepared WORKFLOW.local.md; no service started and no credentials written.')


if __name__ == '__main__':
    render()
