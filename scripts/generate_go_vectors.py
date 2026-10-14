#!/usr/bin/env python3
"""Offline selected-source oracle; default verification never updates vectors."""
# SPDX-License-Identifier: EUPL-1.2
import argparse
import ast
from contextlib import contextmanager
import copy
import hashlib
import itertools
import json
from pathlib import Path
import platform
import re
import signal
import sys

PIN = 'fd0019b7711438a2dbf7255ce3a43572d5a3a6fd'
ROOT = Path(__file__).resolve().parents[1]
DIRECTORY = Path('contracts/migration')
MAX_DOCUMENT = 8 * 1024 * 1024
MAX_BYTES = 2 * 1024 * 1024
MAX_CASES = 256
SOURCES = [
    dict(profile='json', original='scripts/provider_profiles.py', snapshot='oracle/provider_profiles.py',
         revision=PIN, blob='9f5f1e0379c76ed7a704470c787abb0aae9e221a',
         sha256='c199ba25ff55c87474d18a6ccd5e0c03ff529f01678246eea76d566ae27a1026',
         license='EUPL-1.2', imports=['hashlib', 'json'],
         definitions=['MAX_JSON_BYTES', 'ProfileError', '_json_value', '_unique_object',
                      '_reject_number', 'parse_json', 'canonical_bytes', 'canonical_digest']),
    dict(profile='path', original='scripts/single_task_context.py', snapshot='oracle/single_task_context.py',
         revision=PIN, blob='d510d9d0b7fd72b0dd18699a590917e2d45d8b47',
         sha256='87acb4351f4b51e2585228315a36832b4a08ba6ff26bed80698a878dd60c9164',
         license='EUPL-1.2', imports=[],
         definitions=['ContextError', '_require', '_text', 'canonical_path']),
]
RUNTIME = dict(implementation='cpython', version='3.14.8', int_max_str_digits=4300,
               recursion_limit=1000, release='CPython v3.14.8',
               release_reference='https://github.com/python/cpython/tree/v3.14.8',
               provenance='Exact .tool-versions pin and observed CPython sys.version; upstream archive digest not claimed.')


class ContractError(ValueError):
    """Fixed diagnostics never echo fixture content or native exceptions."""


def require(condition, code='invalid migration contract'):
    if not condition:
        raise ContractError(code)


def read_bounded(path, limit=MAX_DOCUMENT):
    path = Path(path)
    require(not path.is_symlink() and path.is_file(), 'expected regular contract file')
    with path.open('rb') as stream:
        data = stream.read(limit + 1)
    require(len(data) <= limit, 'contract file limit exceeded')
    return data


def strict_json(raw):
    require(type(raw) is bytes and len(raw) <= MAX_DOCUMENT, 'fixture document limit exceeded')
    def unique(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, 'duplicate fixture key')
            value[key] = item
        return value
    def constant(unused):
        raise ContractError('nonfinite fixture number')
    try:
        return json.loads(raw.decode('utf-8'), object_pairs_hook=unique, parse_constant=constant)
    except (ValueError, UnicodeError, RecursionError):
        raise ContractError('invalid fixture JSON') from None


def render(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=True, indent=2) + '\n').encode('ascii')


def validate(value, schema, root=None, level=0):
    """Small stdlib validator for the exact checked-in JSON Schema keyword subset."""
    require(level <= 64, 'fixture schema depth exceeded')
    root = schema if root is None else root
    supported = {'$schema', '$id', '$defs', '$ref', 'title', 'description', 'type', 'const', 'enum',
                 'oneOf', 'properties', 'required', 'additionalProperties', 'items',
                 'minItems', 'maxItems', 'minLength', 'maxLength', 'pattern', 'minimum', 'maximum'}
    require(type(schema) is dict and not set(schema) - supported, 'unsupported fixture schema')
    if '$ref' in schema:
        ref = schema['$ref']
        require(type(ref) is str and re.fullmatch(r'#/\$defs/[A-Za-z0-9_]+', ref), 'nonlocal schema reference')
        require(ref[8:] in root['$defs'], 'missing schema reference')
        validate(value, root['$defs'][ref[8:]], root, level + 1)
        return
    if 'oneOf' in schema:
        matches = 0
        for option in schema['oneOf']:
            try:
                validate(value, option, root, level + 1)
                matches += 1
            except ContractError:
                pass
        require(matches == 1, 'fixture schema alternative mismatch')
    types = {'object': dict, 'array': list, 'string': str, 'integer': int, 'boolean': bool, 'null': type(None)}
    if 'type' in schema:
        require(schema['type'] in types and type(value) is types[schema['type']], 'fixture type mismatch')
    if 'const' in schema:
        require(type(value) is type(schema['const']) and value == schema['const'], 'fixture constant mismatch')
    if 'enum' in schema:
        require(any(type(value) is type(item) and value == item for item in schema['enum']), 'fixture enum mismatch')
    if type(value) is dict:
        properties = schema.get('properties', {})
        require(set(schema.get('required', [])) <= set(value), 'missing fixture field')
        if schema.get('additionalProperties') is False:
            require(set(value) <= set(properties), 'unknown fixture field')
        for key, item in value.items():
            if key in properties:
                validate(item, properties[key], root, level + 1)
    if type(value) is list:
        require(schema.get('minItems', 0) <= len(value) <= schema.get('maxItems', 100000), 'fixture item limit')
        if 'items' in schema:
            for item in value:
                validate(item, schema['items'], root, level + 1)
    if type(value) is str:
        require(schema.get('minLength', 0) <= len(value) <= schema.get('maxLength', MAX_DOCUMENT), 'fixture string limit')
        if 'pattern' in schema:
            require(re.fullmatch(schema['pattern'], value) is not None, 'fixture string pattern')
    if type(value) is int:
        require(schema.get('minimum', -sys.maxsize) <= value <= schema.get('maximum', sys.maxsize), 'fixture numeric limit')


def validate_vectors(value, schema):
    validate(value, schema)
    ids = [case['id'] for case in value['cases']]
    require(len(ids) == len(set(ids)), 'duplicate vector identity')
    for case in value['cases']:
        require(('dot' in case) == (case['operation'] == 'canonical-path'), 'operation option mismatch')
        for result in (case['source_expected'], case['product_expected']):
            if result['status'] == 'ok':
                require(('codepoints' in result) == (case['operation'] == 'canonical-path'), 'operation result mismatch')
            else:
                allowed = ('context_schema',) if case['operation'] == 'canonical-path' else ('invalid-json', 'input-limit-exceeded')
                require(result['code'] in allowed, 'operation rejection mismatch')
        allowed_decisions = ('parity', 'scalar-path', 'boolean-dot') if case['operation'] == 'canonical-path' else ('parity', 'depth-128')
        require(case['decision'] in allowed_decisions, 'operation decision mismatch')
        equal = case['source_expected'] == case['product_expected']
        require(equal == (case['decision'] == 'parity'), 'source/product decision mismatch')
        if not equal:
            require(case['source_expected']['status'] == 'ok' and case['product_expected']['status'] == 'reject',
                    'invalid restriction observation')


def load_contract(root=ROOT):
    directory = Path(root)/DIRECTORY
    manifest = strict_json(read_bounded(directory/'manifest.json', 65536))
    require(type(manifest) is dict and set(manifest) == {'schema_version', 'sources', 'runtime', 'files'},
            'invalid provenance fields')
    require(type(manifest['schema_version']) is int and manifest['schema_version'] == 1, 'invalid provenance version')
    require(manifest['sources'] == SOURCES and manifest['runtime'] == RUNTIME, 'provenance identity mismatch')
    files = manifest['files']
    require(type(files) is dict and set(files) == {'vectors.schema.json', 'vectors.json', 'scripts/generate_go_vectors.py'},
            'invalid provenance file inventory')
    require(all(type(value) is str and re.fullmatch('[0-9a-f]{64}', value) for value in files.values()),
            'invalid provenance digest')
    verify_tools(root, manifest)
    schema = strict_json(read_bounded(directory/'vectors.schema.json'))
    vectors = strict_json(read_bounded(directory/'vectors.json'))
    validate_vectors(vectors, schema)
    return manifest, schema, vectors


def verify_tools(root, manifest):
    for name in ('vectors.schema.json', 'scripts/generate_go_vectors.py'):
        path = Path(root)/name if name.startswith('scripts/') else Path(root)/DIRECTORY/name
        require(hashlib.sha256(read_bounded(path)).hexdigest() == manifest['files'][name], 'tool/schema digest mismatch')


def load_oracles(root=ROOT, manifest=None):
    if manifest is None:
        manifest, unused, unused2 = load_contract(root)
    require(manifest['sources'] == SOURCES, 'source provenance mismatch')
    result = {}
    for source in SOURCES:
        raw = read_bounded(Path(root)/DIRECTORY/source['snapshot'], 512 * 1024)
        require(hashlib.sha256(raw).hexdigest() == source['sha256'], 'oracle source digest mismatch')
        blob = b'blob ' + str(len(raw)).encode('ascii') + b'\0' + raw
        require(hashlib.sha1(blob).hexdigest() == source['blob'], 'oracle Git blob mismatch')
        tree = ast.parse(raw, filename=source['original'])
        selected, names, imports = [], [], []
        for node in tree.body:
            if isinstance(node, ast.Import):
                if len(node.names) == 1 and node.names[0].name in source['imports'] and node.names[0].asname is None:
                    selected.append(node); imports.append(node.names[0].name)
            elif isinstance(node, (ast.FunctionDef, ast.ClassDef)) and node.name in source['definitions']:
                selected.append(node); names.append(node.name)
            elif isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
                if node.targets[0].id in source['definitions']:
                    selected.append(node); names.append(node.targets[0].id)
        require(set(names) == set(source['definitions']) and len(names) == len(source['definitions'])
                and sorted(imports) == sorted(source['imports']), 'oracle closure mismatch')
        namespace = {'__name__': 'pinned_migration_' + source['profile']}
        exec(compile(ast.Module(body=selected, type_ignores=[]), source['original'], 'exec'), namespace)
        result[source['profile']] = namespace
    return result


@contextmanager
def oracle_environment():
    require(sys.implementation.name == 'cpython' and tuple(sys.version_info[:3]) == (3, 14, 8),
            'reference requires CPython 3.14.8')
    require(sys.flags.isolated and sys.dont_write_bytecode, 'reference requires -I -B')
    previous = sys.get_int_max_str_digits(), sys.getrecursionlimit()
    try:
        sys.set_int_max_str_digits(4300); sys.setrecursionlimit(1000)
        require(sys.get_int_max_str_digits() == 4300 and sys.getrecursionlimit() == 1000, 'reference settings mismatch')
        yield
    finally:
        sys.set_int_max_str_digits(previous[0]); sys.setrecursionlimit(previous[1])


def encode_bytes(raw):
    require(type(raw) is bytes and len(raw) <= MAX_BYTES, 'expanded bytes limit exceeded')
    result = dict(encoding='hex', hex=raw.hex(), length=len(raw), sha256=hashlib.sha256(raw).hexdigest())
    if len(raw) > 512:
        offset, best = 0, (0, 0)
        for unused, group in itertools.groupby(raw):
            length = sum(1 for unused2 in group)
            if length > best[1]: best = offset, length
            offset += length
        start, length = best
        if length > 512:
            result = dict(encoding='repeat', prefix_hex=raw[:start].hex(), unit_hex=raw[start:start+1].hex(),
                          count=length, suffix_hex=raw[start+length:].hex(), length=len(raw),
                          sha256=hashlib.sha256(raw).hexdigest())
    return result


def expand_bytes(value):
    require(type(value) is dict and value.get('encoding') in ('hex', 'repeat'), 'invalid bytes constructor')
    try:
        if value['encoding'] == 'hex':
            require(set(value) == {'encoding', 'hex', 'length', 'sha256'}, 'invalid hex fields')
            require(type(value['hex']) is str and len(value['hex']) <= MAX_BYTES * 2
                    and re.fullmatch('(?:[0-9a-f]{2})*', value['hex']) is not None, 'invalid hex bytes')
            raw = bytes.fromhex(value['hex'])
        else:
            require(set(value) == {'encoding', 'prefix_hex', 'unit_hex', 'count', 'suffix_hex', 'length', 'sha256'},
                    'invalid repeat fields')
            require(type(value['count']) is int and 0 <= value['count'] <= MAX_BYTES, 'repeat count limit')
            for field in ('prefix_hex', 'unit_hex', 'suffix_hex'):
                require(type(value[field]) is str and len(value[field]) <= MAX_BYTES * 2
                        and re.fullmatch('(?:[0-9a-f]{2})*', value[field]) is not None, 'invalid repeat hex')
            prefix, unit, suffix = (bytes.fromhex(value[field]) for field in ('prefix_hex', 'unit_hex', 'suffix_hex'))
            require(unit and len(prefix) + len(unit)*value['count'] + len(suffix) <= MAX_BYTES, 'expanded bytes limit exceeded')
            raw = prefix + unit*value['count'] + suffix
        require(type(value['length']) is int and len(raw) == value['length'], 'expanded byte length mismatch')
        require(hashlib.sha256(raw).hexdigest() == value['sha256'], 'expanded byte digest mismatch')
        return raw
    except (KeyError, TypeError, ValueError):
        raise ContractError('invalid bytes constructor') from None


def materialize(value, level=0, budget=None):
    budget = [MAX_BYTES] if budget is None else budget
    def charge(amount):
        require(type(amount) is int and 0 <= amount <= budget[0], 'aggregate native construction limit')
        budget[0] -= amount
    charge(64)
    require(level <= 32 and type(value) is dict and type(value.get('kind')) is str, 'native constructor depth/type')
    kind = value['kind']
    if kind == 'null': return None
    if kind == 'boolean':
        require(type(value.get('value')) is bool, 'invalid boolean constructor'); return value['value']
    if kind in ('bytes', 'bytearray'):
        charge(value['data']['length'])
        raw = expand_bytes(value['data']); return raw if kind == 'bytes' else bytearray(raw)
    if kind == 'integer':
        text = value.get('decimal')
        require(type(text) is str and len(text) <= 5001 and re.fullmatch(r'-?(0|[1-9][0-9]*)', text), 'invalid integer constructor')
        charge(len(text))
        negative = text.startswith('-'); digits = text[1:] if negative else text
        # Each int() conversion stays below the oracle's decimal digit limit.
        # Oversized native integers must reach the actual source operation.
        number = 0
        for offset in range(0, len(digits), 9):
            chunk = digits[offset:offset+9]; number = number * 10**len(chunk) + int(chunk)
        return -number if negative else number
    if kind == 'power10':
        require(type(value.get('exponent')) is int and 0 <= value['exponent'] <= 5000
                and type(value.get('negative')) is bool, 'power constructor limit')
        charge(value['exponent'] + 1)
        return (-1 if value['negative'] else 1) * 10**value['exponent']
    if kind == 'string':
        points = value.get('codepoints')
        require(type(points) is list and len(points) <= 8192
                and all(type(point) is int and 0 <= point <= 0x10ffff for point in points), 'invalid codepoint constructor')
        charge(len(points)*4)
        return ''.join(chr(point) for point in points)
    if kind == 'repeat-string':
        point, count = value.get('codepoint'), value.get('count')
        require(type(point) is int and 0 <= point <= 127 and type(count) is int
                and 0 <= count <= 1048577, 'repeated string limit')
        charge(count)
        return chr(point)*count
    if kind == 'float':
        require(value.get('value') in ('1.5', 'nan', 'inf', '-inf'), 'invalid float constructor')
        return float(value['value'])
    if kind in ('list', 'tuple'):
        require(type(value.get('items')) is list and len(value['items']) <= 256, 'native collection limit')
        items = [materialize(item, level+1, budget) for item in value['items']]
        return items if kind == 'list' else tuple(items)
    if kind == 'object':
        require(type(value.get('entries')) is list and len(value['entries']) <= 256, 'native object limit')
        result = {}
        for entry in value['entries']:
            key = materialize(entry['key'], level+1, budget)
            require(type(key) in (str, int, bool, type(None)), 'unhashable constructor key')
            require(key not in result, 'duplicate constructor key')
            result[key] = materialize(entry['value'], level+1, budget)
        return result
    if kind == 'nested':
        depth = value.get('depth'); shape = value.get('shape')
        require(type(depth) is int and 0 <= depth <= 256 and shape in ('array', 'object', 'alternating'), 'nested constructor limit')
        charge(depth*64)
        result = materialize(value['leaf'], level+1, budget)
        for index in range(depth):
            result = {'x': result} if shape == 'object' or (shape == 'alternating' and index % 2) else [result]
        return result
    raise ContractError('unknown native constructor')


def within_depth(value):
    pending = [(value, 0)]
    while pending:
        item, parent_depth = pending.pop()
        if type(item) in (list, dict):
            depth = parent_depth + 1
            if depth > 128: return False
            pending.extend((child, depth) for child in (item.values() if type(item) is dict else item))
    return True


def evaluate(case, oracles, product=False):
    # Construction errors deliberately escape: they are not oracle rejections.
    value = materialize(case['input'])
    operation = case['operation']
    if operation == 'canonical-path':
        dot = materialize(case['dot'])
        if product:
            if type(dot) is not bool: return dict(status='reject', code='context_schema')
            if type(value) is str:
                try: value.encode('utf-8', errors='strict')
                except UnicodeError: return dict(status='reject', code='context_schema')
        try:
            accepted = oracles['path']['canonical_path'](value, dot=dot)
            return dict(status='ok', codepoints=[ord(character) for character in accepted])
        except oracles['path']['ContextError'] as error:
            return dict(status='reject', code=error.code)
    require(operation in ('parse-json', 'canonical-bytes', 'canonical-digest'), 'unknown oracle operation')
    oracle = oracles['json']
    try:
        if operation == 'parse-json': value = oracle['parse_json'](value)
        if product and not within_depth(value): return dict(status='reject', code='invalid-json')
        if operation == 'canonical-digest':
            digest = oracle['canonical_digest'](value)
            raw = oracle['canonical_bytes'](value)
        else:
            raw = oracle['canonical_bytes'](value)
            digest = hashlib.sha256(raw).hexdigest()
        return dict(status='ok', canonical=encode_bytes(raw), digest=digest)
    except oracle['ProfileError'] as error:
        return dict(status='reject', code=error.code)


def reproduce(root=ROOT):
    manifest, schema, vectors = load_contract(root)
    verify_tools(root, manifest)
    result = copy.deepcopy(vectors)
    with oracle_environment():
        oracles = load_oracles(root, manifest)
        for case in result['cases']:
            case['source_expected'] = evaluate(case, oracles)
            case['product_expected'] = evaluate(case, oracles, product=True)
    validate_vectors(result, schema)
    return result


def verify(root=ROOT):
    manifest, unused, unused2 = load_contract(root)
    path = Path(root)/DIRECTORY/'vectors.json'
    original = read_bounded(path)
    require(hashlib.sha256(original).hexdigest() == manifest['files']['vectors.json'], 'frozen vectors digest mismatch')
    result = reproduce(root)
    require(original == render(result), 'frozen vectors differ from oracle')
    return len(result['cases'])


def write_candidate(root, destination):
    root, destination = Path(root).resolve(), Path(destination).resolve()
    require(destination != root and root not in destination.parents, 'candidate must be outside accepted checkout')
    require(not destination.exists(), 'candidate destination already exists')
    result = reproduce(root)
    # Exclusive creation also prevents an accidental overwrite after the check.
    with destination.open('xb') as stream:
        stream.write(render(result))
    return len(result['cases'])


def runtime_evidence():
    executable = Path(sys.executable).resolve()
    return dict(implementation=sys.implementation.name, version=platform.python_version(),
                build=sys.version, platform=platform.platform(), executable=str(executable),
                executable_sha256=hashlib.sha256(read_bounded(executable, 64*1024*1024)).hexdigest(),
                int_max_str_digits=4300, recursion_limit=1000)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, help='write a NEW external candidate; never updates accepted fixtures')
    args = parser.parse_args(argv)
    try:
        require(sys.flags.isolated and sys.dont_write_bytecode, 'reference requires -I -B')
        # Input/file/construction bounds are platform independent. An additional
        # Unix wall deadline bounds this development-only offline command.
        signal.signal(signal.SIGALRM, lambda unused, unused2: (_ for _ in ()).throw(ContractError('oracle deadline exceeded')))
        signal.alarm(30)
        count = write_candidate(ROOT, args.candidate) if args.candidate else verify(ROOT)
        print(json.dumps(dict(mode='candidate' if args.candidate else 'verify', verified_cases=count,
                              runtime=runtime_evidence()), sort_keys=True))
        return 0
    except (ContractError, OSError, ValueError, RecursionError):
        print('Migration verification blocked; require -I -B, pinned runtime and reviewed contract files.', file=sys.stderr)
        return 1
    finally:
        signal.alarm(0)


if __name__ == '__main__':
    raise SystemExit(main())
