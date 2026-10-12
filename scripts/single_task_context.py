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
