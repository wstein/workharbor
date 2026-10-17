#!/usr/bin/env python3
"""Portable scaffold checks; never presented as product acceptance."""
import json
import re
import subprocess
import sys
from pathlib import Path

WORKSPACE_MANIFEST = 'examples/workspace/workharbor.json'

REQUIRED = [WORKSPACE_MANIFEST, 'README.md', 'AGENTS.md', '.specify/memory/constitution.md',
            'WORKFLOW.md', 'docs/index.md', 'contracts/README.md',
            'specs/001-workspace-lifecycle/spec.md',
            'specs/001-workspace-lifecycle/plan.md',
            'specs/001-workspace-lifecycle/tasks.md']

def document_errors(root, path):
    errors = []
    text = path.read_text()
    # Code fences can contain illustrative links that are not document navigation.
    text = re.sub(r'```.*?```', '', text, flags=re.S)
    for target in re.findall(r'\[[^\]]+\]\(([^)]+)\)', text):
        if re.match(r'^(https?://|mailto:|#)', target):
            continue
        target = target.split('#', 1)[0]
        if target and not (path.parent / target).exists():
            errors.append(f'{path.relative_to(root)}: broken link {target}; fix the path or create its target')
    return errors

SCHEMA_PATH = 'contracts/workspace/workharbor.schema.json'
VECTORS_PATH = 'contracts/workspace/vectors.json'
MANIFEST_MAX_BYTES = 65536
MANIFEST_MAX_DEPTH = 128
V1_FIELDS = ('schema_version', 'devcontainer_path', 'projects_path', 'bundles')
V2_OPTIONAL = ('name', 'start_dir', 'human_shell', 'console_mode', 'shortcuts', 'project')
SCHEMA_ROOT = Path(__file__).resolve().parents[1]


def _schema(root=None):
    path = Path(root or SCHEMA_ROOT)/SCHEMA_PATH
    if not path.is_file():
        path = SCHEMA_ROOT/SCHEMA_PATH
    return json.loads(path.read_text(encoding='utf-8'))


def _too_deep(text):
    """Iterative nesting scan: no recursion, so a depth bomb cannot exhaust the stack."""
    depth = 0
    in_string = escaped = False
    for char in text:
        if in_string:
            if escaped:
                escaped = False
            elif char == '\\':
                escaped = True
            elif char == '"':
                in_string = False
        elif char == '"':
            in_string = True
        elif char in '[{':
            depth += 1
            if depth > MANIFEST_MAX_DEPTH:
                return True
        elif char in ']}':
            depth -= 1
    return False


def _no_constant(name):
    raise ValueError(name)


class _BigInt:
    """An integer too long to compare with anything: never equal to a schema constant."""


def _parse_int(text):
    # Independent of the interpreter's int/str digit limit; the 64 KiB bound keeps this cheap.
    return int(text) if len(text) <= 18 else _BigInt()


class _Seen:
    """Parse-time facts collected by the object hook: a repeated key and any lone surrogate."""

    def __init__(self):
        self.duplicate = False
        self.surrogate = False

    def pairs(self, pairs):
        keys = [key for key, _ in pairs]
        if len(keys) != len(set(keys)):
            self.duplicate = True
        # Inspect every pair, including values a duplicate key would drop.
        if not self.surrogate and _has_lone_surrogate([item for pair in pairs for item in pair
                                                       if not isinstance(item, (dict, list))]):
            self.surrogate = True
        return dict(pairs)


def _has_lone_surrogate(value):
    stack = [value]
    while stack:
        item = stack.pop()
        if isinstance(item, str):
            if _SURROGATE.search(item):
                return True
        elif isinstance(item, dict):
            stack.extend(item.keys())
            stack.extend(item.values())
        elif isinstance(item, list):
            stack.extend(item)
    return False


_SURROGATE = re.compile('[\ud800-\udfff]')
# Order of the manifest.* codes in contracts/cli/validate-findings.json.
CODE_ORDER = ('manifest.too_large', 'manifest.encoding', 'manifest.syntax', 'manifest.duplicate_key',
              'manifest.schema', 'manifest.path_invalid', 'manifest.url_credentials',
              'manifest.bundles_deferred')


def manifest_findings(content, schema=None):
    """(code, field, detail) tuples for the raw bytes of a manifest; empty means valid.

    Version comparison is type-exact: True, 1.0 and 1e0 are not 1.
    """
    if len(content) > MANIFEST_MAX_BYTES:
        return [('manifest.too_large', '', '')]
    if content.startswith(b'\xef\xbb\xbf') or b'\x00' in content:
        return [('manifest.encoding', '', '')]
    try:
        text = content.decode('utf-8')
    except UnicodeDecodeError:
        return [('manifest.encoding', '', '')]
    if _too_deep(text):
        return [('manifest.syntax', '', '')]
    seen = _Seen()
    try:
        data = json.loads(text, object_pairs_hook=seen.pairs, parse_constant=_no_constant,
                          parse_int=_parse_int)
    except (ValueError, RecursionError):
        return [('manifest.syntax', '', '')]
    if seen.surrogate or _has_lone_surrogate(data):
        return [('manifest.encoding', '', 'lone surrogate')]
    if seen.duplicate:
        return [('manifest.duplicate_key', '', '')]
    return manifest_document_findings(data, schema)


def manifest_codes(content, schema=None):
    """Distinct finding codes in catalogue order (see CODE_ORDER); empty means valid."""
    codes = {code for code, _, _ in manifest_findings(content, schema)}
    return [code for code in CODE_ORDER if code in codes]


def _is_int(value, expected):
    return type(value) is int and value == expected


def _versions(schema):
    """Map each accepted schema_version to its definition, following the schema's oneOf."""
    defs = schema['$defs']
    result = {}
    for branch in schema['oneOf']:
        definition = defs[branch['$ref'].rsplit('/', 1)[-1]]
        result[definition['properties']['schema_version']['const']] = definition
    return result


# Characters that Windows or macOS file systems drop or fold before comparing a name.
_IGNORED = re.compile('[\u200b-\u200f\u202a-\u202e\u206a-\u206f\ufeff]')
_FOLDED = str.maketrans({'\uff0e': '.', '\u00a0': ' ', '\u3000': ' '})


def _windows_alias(component):
    """True for a component that a Windows-style file system may map to . , .. or .git.

    Mirrors isWindowsAlias in apps/whr/internal/workspacecfg/paths.go: after dropping
    ignorable format characters and folding U+FF0E, U+00A0 and U+3000, a component of
    only dots and spaces, or whose stream-free base (text before the first colon) is .git
    or the 8.3 short name GIT~1 once trailing dots and spaces are removed, is refused.
    """
    folded = _IGNORED.sub('', component).translate(_FOLDED)
    if not folded.strip('. '):
        return True
    base = folded.split(':', 1)[0].rstrip('. ').casefold()
    return base in ('.git', 'git~1')


def manifest_document_findings(data, schema=None):
    """(code, field, detail) tuples for a parsed manifest value."""
    schema = schema or _schema()
    defs = schema['$defs']
    findings = []

    def add(code, field, detail=''):
        findings.append((code, field, detail))

    if not isinstance(data, dict):
        return [('manifest.schema', '', 'require a JSON object')]
    versions = _versions(schema)
    version = data.get('schema_version')
    definition = next((d for v, d in versions.items() if _is_int(version, v)), None)
    if definition is None:
        return [('manifest.schema', 'schema_version', 'require a supported integer version')]
    properties = definition['properties']
    if (not set(definition['required']) <= set(data)
            or (definition.get('additionalProperties') is False and set(data) - set(properties))):
        add('manifest.schema', '', 'unexpected or missing fields')
    path_pattern = re.compile(defs['canonicalPath']['pattern'])

    def canonical(value):
        return (isinstance(value, str) and path_pattern.fullmatch(value) is not None
                and not any(_windows_alias(part) for part in value.split('/')))

    for field in ('devcontainer_path', 'projects_path'):
        if field in data:
            if not isinstance(data[field], str):
                add('manifest.schema', field, 'require a string')
            elif not canonical(data[field]):
                add('manifest.path_invalid', field)
    if 'bundles' in data:
        if not isinstance(data['bundles'], list):
            add('manifest.schema', 'bundles', 'require a list')
        elif len(data['bundles']) > properties['bundles'].get('maxItems', len(data['bundles'])):
            add('manifest.bundles_deferred', 'bundles')
    if 'name' in data and 'name' in properties:
        if not (isinstance(data['name'], str) and re.fullmatch(properties['name']['pattern'], data['name'])):
            add('manifest.schema', 'name')
    if 'start_dir' in data and 'start_dir' in properties:
        value = data['start_dir']
        if not isinstance(value, str):
            add('manifest.schema', 'start_dir', 'require a string')
        elif value != '.' and not canonical(value):
            add('manifest.path_invalid', 'start_dir')
    for field in ('human_shell', 'console_mode', 'shortcuts'):
        if field in data and field in properties and not (
                isinstance(data[field], str) and data[field] in properties[field]['enum']):
            add('manifest.schema', field)
    if 'project' in data and 'project' in properties:
        findings.extend(_project_findings(data['project'], data.get('projects_path'), properties['project'],
                                          canonical(data.get('projects_path'))))
    return findings


_URL_PARTS = re.compile(r'(?:https|ssh)://(?:[^/@]*@)?([^/:@]+)(?::([0-9]+))?(/.*)')


def _url_authority(url):
    """(scheme, userinfo-with-@ or '', host-with-port, rest) of scheme://authority/rest, or None."""
    index = url.find('://')
    if index <= 0:
        return None
    scheme, tail = url[:index], url[index + 3:]
    cut = re.search('[/?#]', tail)
    auth, rest = (tail[:cut.start()], tail[cut.start():]) if cut else (tail, '')
    at = auth.rfind('@')
    if at < 0:
        return scheme, '', auth, rest
    return scheme, auth[:at + 1], auth[at + 1:], rest


def _has_credentials(url):
    """Any http(s) userinfo, or an ssh user part with a password; the scheme is case-insensitive."""
    parts = _url_authority(url)
    if parts is None or not parts[1]:
        return False
    scheme = parts[0].lower()
    return scheme in ('https', 'http') or (scheme == 'ssh' and ':' in parts[1])


def _project_findings(project, projects_path, project_schema, path_ok=True):
    if not isinstance(project, dict) or set(project) != {'url', 'path'}:
        return [('manifest.schema', 'project', 'require exactly url and path')]
    url, path = project['url'], project['path']
    if not isinstance(url, str) or not isinstance(path, str):
        return [('manifest.schema', 'project', 'require strings')]
    if _has_credentials(url):
        return [('manifest.url_credentials', 'project.url', '')]
    problems = []
    url_schema = project_schema['properties']['url']
    parts = _URL_PARTS.fullmatch(url)
    authority = _url_authority(url)
    if (not re.fullmatch(url_schema['pattern'], url) or len(url) > url_schema['maxLength']
            or re.search(r'%(?![0-9A-Fa-f]{2})', url) or parts is None
            or (parts.group(2) is not None and not 1 <= int(parts.group(2)) <= 65535)
            or '//' in parts.group(3)
            or authority[1].startswith('-') or authority[2].startswith('-')):
        problems.append(('manifest.schema', 'project.url', ''))
    last = '' if url.endswith('/') else url.rsplit('/', 1)[-1]
    name = last[:-4] if last.endswith('.git') else last
    # With an unusable projects_path (already reported) there is nothing to compare against.
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]*', name) or (path_ok and path != f'{projects_path}/{name}'):
        problems.append(('manifest.schema', 'project.path', ''))
    return problems


def vector_bytes(case):
    """Materialize the raw manifest bytes of one shared vector case."""
    if 'document' in case:
        return json.dumps(case['document']).encode('utf-8')
    if 'bytes_hex' in case:
        return bytes.fromhex(case['bytes_hex'])
    if 'raw_text' in case:
        return case['raw_text'].encode('utf-8')
    spec = case['generate_bytes']
    template = spec['template'].encode('utf-8')
    return template + b' ' * (spec['total'] - len(template))


MIN_VECTOR_CASES = 101


def vector_case_errors(cases):
    errors = []
    seen = set()
    for case in cases:
        if case['id'] in seen:
            errors.append(f'{VECTORS_PATH}: duplicate case id {case["id"]}; ids are unique')
        seen.add(case['id'])
        actual = manifest_codes(vector_bytes(case))
        if sorted(actual) != sorted(case['expect_codes']):
            errors.append(f'{VECTORS_PATH}: case {case["id"]} expects {case["expect_codes"]} but check_repo.py '
                          f'yields {actual}; align the rule and the vector')
    return errors


def vector_errors(root):
    path = Path(root)/VECTORS_PATH
    if not path.is_file():
        return [f'{VECTORS_PATH}: required vectors missing; restore the shared file']
    try:
        cases = json.loads(path.read_text(encoding='utf-8'))['cases']
        errors = vector_case_errors(cases)
    except (ValueError, KeyError, TypeError, OSError, RecursionError) as exc:
        return [f'{VECTORS_PATH}: malformed vector file; repair it ({exc})']
    if len(cases) < MIN_VECTOR_CASES:
        errors.append(f'{VECTORS_PATH}: {len(cases)} cases, fewer than the pinned {MIN_VECTOR_CASES}; '
                      'cases are add-only')
    return errors


def _example_message(code, field, detail):
    """The historical workspace-example wording, kept verbatim for the old failure classes."""
    if code == 'manifest.path_invalid' or (code == 'manifest.schema' and field in ('devcontainer_path', 'projects_path')):
        return f'workspace example: unsafe {field}; use a relative path without traversal'
    if code == 'manifest.bundles_deferred' or field == 'bundles':
        return 'workspace example: attachments are deferred; retain the empty bundle list'
    if code == 'manifest.schema' and field in ('', 'schema_version') and detail.startswith('require a'):
        return 'workspace example: require schema_version 1'
    if code == 'manifest.schema' and field == '':
        return 'workspace example: unexpected or missing fields; update the reviewed contract'
    label = ' '.join(part for part in (code, field, detail) if part)
    return f'workspace example: {label}; update the reviewed contract'


def workspace_errors(data):
    return list(dict.fromkeys(_example_message(*f) for f in manifest_document_findings(data)))


def example_errors(root, path):
    """Strict byte-level validation of one example manifest, reported once."""
    relative = path.relative_to(root)
    findings = manifest_findings(path.read_bytes(), _schema(root))
    if any(code in ('manifest.too_large', 'manifest.encoding', 'manifest.syntax', 'manifest.duplicate_key')
           for code, _, _ in findings):
        return [f'{relative}: invalid JSON; repair syntax ({findings[0][0]})']
    return [e.replace('workspace example:', f'{relative}:', 1) if path != root/WORKSPACE_MANIFEST else e
            for e in dict.fromkeys(_example_message(*f) for f in findings)]

def boundary_errors(relative, text):
    errors = []
    path = str(relative)
    if path.startswith('apps/control_plane/') and re.search(r'docker\.sock|github\.com/(docker|moby)/|DockerProvider', text):
        errors.append(f'{path}: provider implementation leaked into control plane; call a port')
    if path.startswith('apps/host_agent/') and '/providers/' not in path:
        # These exact pure input-grammar imports implement the reviewed config
        # capability gate; they expose no daemon/provider access. No module-prefix
        # exception, alias, comment, mixed statement or other file is admitted.
        grammar = 'github.com/moby/buildkit/frontend/dockerfile/'
        pure_imports = {
            'apps/host_agent/internal/config/dockerfile_plan.go': {'parser'},
            'apps/host_agent/internal/config/trusted.go': {'parser', 'instructions', 'linter'},
        }
        allowed_lines = {'"' + grammar + name + '"' for name in pure_imports.get(path, set())}
        import_scan = ''.join(line for line in text.splitlines(keepends=True)
                              if line.strip() not in allowed_lines)
        if re.search(r'"github\.com/(docker|moby)/', import_scan) or re.search(r'docker\.sock', text):
            errors.append(f'{path}: Docker access outside provider adapter; move it under providers/')
    if path.startswith('apps/') and re.search(r'openai/symphony|codex app-server', text):
        errors.append(f'{path}: development orchestrator entered product code; remove the dependency')
    return errors

def check(root):
    errors = [f'{name}: required artifact missing; restore it' for name in REQUIRED if not (root/name).is_file()]
    manifests = {root/WORKSPACE_MANIFEST, *(root/'examples/workspace').rglob('workharbor.json')}
    for manifest in sorted(m for m in manifests if m.is_file() and not m.is_symlink()):
        errors.extend(example_errors(root, manifest))
    errors.extend(vector_errors(root))
    tracked = subprocess.check_output(['git', 'ls-files', '-z'], cwd=root).decode().split('\0')
    for name in filter(None, tracked):
        path = root/name
        if not path.is_file():
            errors.append(f'{name}: tracked file missing; restore or remove deliberately')
            continue
        if name.endswith('.json') and path not in manifests:
            try:
                data = json.loads(path.read_text())
            except (ValueError, UnicodeError, RecursionError) as exc:
                errors.append(f'{name}: invalid JSON; repair syntax ({exc})')
        if name.endswith('.md') and (name.startswith(('docs/', 'specs/', 'contracts/', 'examples/')) or name in ('README.md','AGENTS.md')):
            errors.extend(document_errors(root, path))
        if name.startswith('apps/') and path.suffix in ('.go', '.ex', '.exs'):
            errors.extend(boundary_errors(Path(name), path.read_text()))
        if re.search(r'(^|/)(\.env|auth\.json)$', name):
            errors.append(f'{name}: credential file tracked; remove it and rotate exposed credentials')
    if (root/'WORKFLOW.md').exists():
        try:
            _, front, body = (root/'WORKFLOW.md').read_text().split('---', 2)
            workflow = json.loads(front)
            if workflow['tracker']['provider']['token'] != '$GITHUB_TOKEN':
                errors.append('WORKFLOW.md: use a secret reference, never an inline credential')
            tracker = workflow['tracker']
            if (tracker['kind'] != 'github' or tracker['active_states'] != ['open']
                    or tracker['terminal_states'] != ['closed']
                    or tracker['required_labels'] != ['workharbor-ready']):
                errors.append('WORKFLOW.md: dispatch only open GitHub issues labeled workharbor-ready')
            if workflow['agent']['max_concurrent_agents'] > 2:
                errors.append('WORKFLOW.md: baseline concurrency exceeds two; review capacity first')
            if not body.strip():
                errors.append('WORKFLOW.md: missing agent policy')
        except (ValueError, KeyError, TypeError) as exc:
            errors.append(f'WORKFLOW.md: malformed baseline contract; restore JSON YAML front matter ({exc})')
    if (root/'AGENTS.md').exists() and len((root/'AGENTS.md').read_text().splitlines()) > 100:
        errors.append('AGENTS.md: exceeds 100 lines; move detail into linked documents')
    return errors

if __name__ == '__main__':
    errors = check(Path(__file__).resolve().parents[1])
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        sys.exit(1)
    print('Repository bootstrap checks passed (product runtime not evaluated).')
