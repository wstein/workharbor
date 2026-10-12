"""Strict local TaskContext schema and canonical identities for Feature 003.

References are operator-supplied local assertions, never authenticated ownership.
Schema validation executes no commands and changes no files or caller-owned data.
"""
import copy
import hashlib
import json
import math
import re
from pathlib import Path

TASK_ID = re.compile(r'T[0-9]{3}\Z')
SHA256 = re.compile(r'[0-9a-f]{64}\Z')
ARTIFACTS = ('spec.md', 'plan.md', 'tasks.md')
CONTEXT_KEYS = {'schema_version', 'feature', 'task_id', 'mode', 'reviewed_artifacts',
                'approval', 'dependencies', 'source_assertion', 'prerequisite_evidence',
                'allowed_paths', 'semantic_constraints', 'checks'}
REWORK_KEYS = {'prior_attempt_id', 'prior_implementation_commit', 'rework_approval'}


class ContextError(ValueError):
    """Safe machine-readable rejection without reproducing supplied values."""
    def __init__(self, code='context_schema'):
        self.code = code
        super().__init__('Task context validation failed; review the approved local inputs.')


def _require(condition):
    if not condition:
        raise ContextError()


def _record(value, keys):
    _require(isinstance(value, dict) and set(value) == set(keys))


def _text(value):
    return isinstance(value, str) and bool(value) and '\0' not in value


def canonical_bytes(value):
    """UTF-8 JSON; no Unicode normalization, nonfinite values or trailing LF."""
    try:
        return json.dumps(value, sort_keys=True, separators=(',', ':'),
                          ensure_ascii=False, allow_nan=False).encode('utf-8')
    except (TypeError, ValueError, UnicodeError):
        raise ContextError() from None


def context_digest(context):
    value = copy.deepcopy(context)
    _require(isinstance(value, dict) and isinstance(value.get('approval'), dict))
    value['approval'].pop('context_digest', None)
    return hashlib.sha256(canonical_bytes(value)).hexdigest()


def canonical_path(value, *, dot=False):
    """Validate spelling without resolving, normalizing or following a path."""
    if dot and value == '.':
        return value
    _require(_text(value) and not value.startswith('/') and '\\' not in value
             and not any(character in value for character in '\r\n\t*?[]'))
    parts = value.split('/')
    _require(all(part and part not in ('.', '..', '.git') for part in parts))
    return value


def _feature(value):
    canonical_path(value)
    _require('/' not in value)


def _identifier(value):
    return isinstance(value, str) and TASK_ID.fullmatch(value) is not None


def _sha(value):
    return isinstance(value, str) and SHA256.fullmatch(value) is not None


def _argv(value):
    _require(isinstance(value, list) and bool(value) and all(_text(item) for item in value))


def _manifest_shape(value):
    _record(value, {'schema_version', 'entries'})
    _require(type(value['schema_version']) is int and value['schema_version'] == 1
             and isinstance(value['entries'], list))
    paths = []
    for entry in value['entries']:
        _record(entry, {'path', 'type', 'mode', 'sha256'})
        paths.append(canonical_path(entry['path']))
        if entry['type'] == 'regular':
            _require(entry['mode'] in ('100644', '100755') and _sha(entry['sha256']))
        else:
            _require(entry['type'] == 'absent' and entry['mode'] is None and entry['sha256'] is None)
    _require(paths == sorted(set(paths)))


def _prerequisite_shape(value):
    _record(value, {'schema_version', 'feature', 'task_id', 'completed', 'content_manifest',
                    'implementation_commit', 'check_results', 'evidence_ref'})
    _require(type(value['schema_version']) is int and value['schema_version'] == 1
             and _identifier(value['task_id']) and type(value['completed']) is bool
             and _text(value['implementation_commit']) and _text(value['evidence_ref'])
             and isinstance(value['check_results'], list) and bool(value['check_results']))
    _feature(value['feature'])
    _manifest_shape(value['content_manifest'])
    ids = []
    for check in value['check_results']:
        _record(check, {'id', 'argv', 'cwd', 'exit', 'content_manifest_digest'})
        _require(_text(check['id']) and type(check['exit']) is int and _sha(check['content_manifest_digest']))
        ids.append(check['id'])
        _argv(check['argv'])
        canonical_path(check['cwd'], dot=True)
    _require(len(ids) == len(set(ids)))


def validate_context(value):
    """Validate schema only; filesystem/approval/prerequisite gates are separate."""
    _require(isinstance(value, dict))
    mode = value.get('mode')
    _require(mode in ('implement', 'owner-authorized rework'))
    _record(value, CONTEXT_KEYS | (REWORK_KEYS if mode == 'owner-authorized rework' else set()))
    _require(type(value['schema_version']) is int and value['schema_version'] == 1)
    feature = value['feature']
    _feature(feature)
    _require(_identifier(value['task_id']))
    _record(value['reviewed_artifacts'], ARTIFACTS)
    for name in ARTIFACTS:
        record = value['reviewed_artifacts'][name]
        _record(record, {'path', 'commit', 'sha256'})
        _require(record['path'] == f'specs/{feature}/{name}' and _text(record['commit']) and _sha(record['sha256']))
        canonical_path(record['path'])
    approval = value['approval']
    _record(approval, {'owner_ref', 'evidence_ref', 'context_digest'})
    _require(_text(approval['owner_ref']) and _text(approval['evidence_ref']) and _sha(approval['context_digest']))
    dependencies = value['dependencies']
    _require(isinstance(dependencies, list) and all(_identifier(item) for item in dependencies))
    _require(dependencies == sorted(set(dependencies)))
    source = value['source_assertion']
    _record(source, {'path', 'sha256', 'excerpt', 'owner_ref'})
    _require(source['path'] == f'specs/{feature}/tasks.md' and _sha(source['sha256'])
             and _text(source['excerpt']) and _text(source['owner_ref']))
    canonical_path(source['path'])
    evidence = value['prerequisite_evidence']
    _require(isinstance(evidence, list))
    for record in evidence:
        _prerequisite_shape(record)
    paths = value['allowed_paths']
    _require(isinstance(paths, list) and bool(paths))
    protected = {f'specs/{feature}/{name}' for name in ARTIFACTS}
    for path in paths:
        canonical_path(path)
        _require(path not in protected and path.split('/')[0] != '.symphony'
                 and not any(path == prefix or path.startswith(prefix + '/') for prefix in
                             ('specs/001-workspace-lifecycle', 'specs/002-symphony-doc-smoke')))
    _require(len(paths) == len(set(paths)))
    descriptions = value['semantic_constraints']
    _require(isinstance(descriptions, list) and bool(descriptions) and all(_text(item) for item in descriptions))
    checks = value['checks']
    _require(isinstance(checks, list) and bool(checks))
    ids = []
    for check in checks:
        _record(check, {'id', 'argv', 'cwd', 'timeout_seconds', 'expected_exit'})
        _require(_text(check['id']))
        ids.append(check['id'])
        _argv(check['argv'])
        canonical_path(check['cwd'], dot=True)
        timeout = check['timeout_seconds']
        try:
            finite = type(timeout) in (int, float) and math.isfinite(timeout) and timeout > 0
        except OverflowError:
            finite = False
        _require(finite and type(check['expected_exit']) is int and check['expected_exit'] == 0)
    _require(len(ids) == len(set(ids)))
    if mode == 'owner-authorized rework':
        _require(_text(value['prior_attempt_id']) and _text(value['prior_implementation_commit']))
        _record(value['rework_approval'], {'owner_ref', 'evidence_ref'})
        _require(all(_text(item) for item in value['rework_approval'].values()))
    canonical_bytes(value)
    return copy.deepcopy(value)


def load_context(path):
    """Read strict UTF-8 JSON; duplicate keys and constants are invalid input."""
    def unique(pairs):
        result = {}
        for key, item in pairs:
            if key in result:
                raise ContextError('invalid_json')
            result[key] = item
        return result

    def constant(value):
        raise ContextError('invalid_json')

    try:
        value = json.loads(Path(path).read_text(encoding='utf-8'),
                           object_pairs_hook=unique, parse_constant=constant)
    except ContextError:
        raise
    except (OSError, ValueError, UnicodeError):
        raise ContextError('invalid_json') from None
    return validate_context(value)


def reviewed_bytes(root, record):
    """Resolve a reviewed ancestor and read its exact blob without fetching."""
    from single_task_scope import git
    try:
        commit = git(root, 'rev-parse', '--verify', '--end-of-options',
                     record['commit'] + '^{commit}').stdout.strip().decode('ascii')
        if git(root, 'merge-base', '--is-ancestor', commit, 'HEAD', allowed=(0, 1)).returncode:
            raise ContextError('artifact_commit')
        entry = git(root, 'ls-tree', '-z', commit, '--', record['path']).stdout
        metadata, path = entry.rstrip(b'\0').split(b'\t')
        mode, kind, identity = metadata.split()
        if mode not in (b'100644', b'100755') or kind != b'blob' or path.decode('utf-8') != record['path']:
            raise ContextError('artifact_commit')
        content = git(root, 'cat-file', 'blob', identity.decode('ascii')).stdout
    except (ContextError, UnicodeError, ValueError):
        raise ContextError('artifact_commit') from None
    if hashlib.sha256(content).hexdigest() != record['sha256']:
        raise ContextError('artifact_digest')
    return content


def _footer_parts(content):
    """Recognize a terminal structured footer only outside Markdown fences."""
    offset = 0
    fence = None
    start = None
    for line in content.splitlines(keepends=True):
        marker = re.match(rb' {0,3}(`{3,}|~{3,})([^\r\n]*)', line)
        if fence:
            if marker and marker[1][:1] == fence[0] and len(marker[1]) >= fence[1] and not marker[2].strip():
                fence = None
        elif marker and not (marker[1][:1] == b'`' and b'`' in marker[2]):
            fence = (marker[1][:1], len(marker[1]))
        elif line in (b'## Execution evidence\n', b'## Execution evidence\r\n'):
            start = offset
        offset += len(line)
    if start is None:
        return content, None, []
    footer = content[start:]
    lines = footer.splitlines(keepends=True)
    records = []
    for line in lines[1:]:
        record = re.fullmatch(rb'- Evidence: (T[0-9]{3}) ([^ \t\r\n]+) ([0-9a-f]{64})(?:\r?\n)?', line)
        if record is None:
            return content, None, []
        records.append(record.groups())
    if not records:
        return content, None, []
    return content[:start], footer, records


def _approval_body(content, tasks):
    if not tasks:
        return content
    from single_task_scope import task_lines
    value = bytearray(content)
    for task in task_lines(content).values():
        value[task['marker_offset']] = ord(' ')
    return bytes(value)


def validate_reviewed_artifacts(root, value):
    """Bind approval and compare prose, permitting only documented progress."""
    from single_task_scope import read_regular, task_lines
    context = validate_context(value)
    if context_digest(context) != context['approval']['context_digest']:
        raise ContextError('approval_digest')
    reviewed = {name: reviewed_bytes(root, record)
                for name, record in context['reviewed_artifacts'].items()}
    try:
        current = {name: read_regular(root, record['path'])
                   for name, record in context['reviewed_artifacts'].items()}
        tasks = task_lines(current['tasks.md'])
        for name in ARTIFACTS:
            before, old_footer, old_records = _footer_parts(reviewed[name])
            after, new_footer, new_records = _footer_parts(current[name])
            before = _approval_body(before, name == 'tasks.md')
            after = _approval_body(after, name == 'tasks.md')
            if old_footer is not None:
                matches = after == before and new_footer is not None and new_footer.startswith(old_footer)
            elif new_footer is not None:
                newline = b'\r\n' if new_footer.startswith(b'## Execution evidence\r\n') else b'\n'
                matches = after in (before, before + newline)
            else:
                matches = after == before
            if not matches:
                raise ContextError('artifact_drift')
            for task, path, digest in old_records + new_records:
                path = path.decode('utf-8')
                canonical_path(path)
                if task.decode('ascii') not in tasks or hashlib.sha256(read_regular(root, path)).hexdigest().encode() != digest:
                    raise ContextError('artifact_drift')
    except (ContextError, UnicodeError, OSError, ValueError):
        raise ContextError('artifact_drift') from None
    return reviewed
