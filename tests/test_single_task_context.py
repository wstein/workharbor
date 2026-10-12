"""Feature 003 context contract against disposable, offline Git repositories.

The T005 matrix is reviewed separately. Missing validators are observed through
real deferred entry, never failed imports, skips or synthetic unittest failures.
"""
import copy
import importlib.util
import json
import os
import re
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from test_single_task import SingleTaskFixture, canonical_bytes, context_digest, digest

ROOT = Path(__file__).resolve().parents[1]


class TaskContextEntryTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        self.tasks = self.fixture.feature/'tasks.md'
        self.context = self.fixture.context()
        self.context['checks'] = [{'id': 'must-not-run', 'argv': [sys.executable, '-c',
                                  'from pathlib import Path; Path("acceptance-ran").touch()'],
                                  'cwd': '.', 'timeout_seconds': 10, 'expected_exit': 0}]
        self.seal()
        self.input = self.fixture.private/'context.json'
        self.raw = None
        (self.root/'.symphony').mkdir()
        (self.root/'.symphony/prior-evidence').write_bytes(b'Retain prior local evidence\n')
        (self.root/'.git/info').mkdir(exist_ok=True)
        with (self.root/'.git/info/exclude').open('a') as exclude:
            exclude.write('\n.symphony/\n')

    def seal(self):
        if 'approval' in self.context:
            self.context['approval']['context_digest'] = context_digest(self.context)

    def commit(self):
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: prepare context scenario')

    def review_current(self):
        self.commit()
        commit = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        self.context['reviewed_artifacts'] = {
            name: {'path': f'specs/{self.context["feature"]}/{name}', 'commit': commit,
                   'sha256': digest((self.fixture.feature/name).read_bytes())}
            for name in ('spec.md', 'plan.md', 'tasks.md')
        }
        self.context['source_assertion']['sha256'] = digest(self.tasks.read_bytes())
        lines = self.tasks.read_text().splitlines()
        try:
            start = lines.index('| Task | Prerequisites |')
            end = start + 2
            while end < len(lines) and lines[end].startswith('|'):
                end += 1
            excerpt = '\n'.join(lines[start:end])
        except ValueError:
            excerpt = self.fixture.dependency_table
        self.context['source_assertion']['excerpt'] = excerpt
        self.seal()

    def dependency(self):
        prerequisite = self.fixture.complete_prerequisite()
        checks = self.context['checks']
        self.context = self.fixture.context('T002', prerequisite)
        self.context['checks'] = checks
        self.seal()

    def replace_tasks(self, before, after, reviewed=True):
        self.tasks.write_bytes(self.tasks.read_bytes().replace(before.encode(), after.encode()))
        if reviewed:
            self.review_current()
        else:
            self.commit()

    def footer(self):
        return '\n## Execution evidence\n- Evidence: T001 guide.md ' + digest((self.root/'guide.md').read_bytes()) + '\n'

    def snapshot(self, include_state=True):
        entries = {}
        for path in self.root.rglob('*'):
            relative = path.relative_to(self.root).as_posix()
            if relative in {'.git', '.symphony', '.symphony/single-task', '.symphony/single-task/lock'} or relative.startswith('.git/'):
                continue
            if not include_state and relative.startswith('.symphony/'):
                continue
            if path.is_symlink():
                entries[relative] = ('symlink', os.readlink(path))
            elif path.is_file():
                entries[relative] = (path.stat().st_mode, path.read_bytes())
            else:
                entries[relative] = ('directory',)
        return (entries, (self.root/'.git/index').read_bytes(),
                self.fixture.git('rev-parse', 'HEAD').stdout,
                self.fixture.git('ls-tree', '-r', 'HEAD').stdout)

    def module(self, name):
        path = ROOT/'scripts'/f'{name}.py'
        if not path.is_file():
            return None
        sys.path.insert(0, str(ROOT/'scripts'))
        self.addCleanup(sys.path.remove, str(ROOT/'scripts'))
        spec = importlib.util.spec_from_file_location(name, path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def deferred(self, expected_code):
        """Observe actual missing entry behavior after fixture preparation."""
        self.input.write_bytes(self.raw if self.raw is not None else canonical_bytes(self.context))
        before = self.snapshot()
        product_before = self.snapshot(include_state=False)
        result = subprocess.run([sys.executable, '-B', str(ROOT/'scripts/single_task.py'),
                                 'begin', '--context', str(self.input)], cwd=self.root,
                                env=self.fixture.env, capture_output=True, text=True, timeout=10)
        response = json.loads(result.stdout)
        self.assertEqual(product_before, self.snapshot(include_state=False))
        self.assertEqual(b'Retain prior local evidence\n', (self.root/'.symphony/prior-evidence').read_bytes())
        if result.returncode:
            self.assertEqual(before, self.snapshot())
        self.assertFalse((self.root/'acceptance-ran').exists())
        self.assertFalse(response['owner_identity_authenticated'])
        self.assertFalse(response['live_dispatch_verified'])
        self.assertEqual(1, response['schema_version'])
        self.assertEqual('', result.stderr)
        if expected_code:
            self.assertEqual(2, result.returncode)
            self.assertEqual(expected_code, response['errors'][0]['code'])
        else:
            self.assertEqual(0, result.returncode)
            self.assertEqual('implement', response['mode'])
            self.assertEqual('active', response['progress'])
            self.assertTrue(response['attempt_id'])
            self.assertEqual([], response['errors'])
            state = self.module('single_task_state')
            with state.open_store(self.root) as store:
                attempt = store.read_attempt(response['attempt_id'])
                self.assertEqual(self.context, attempt['context'])
                self.assertEqual(product_before[2].strip(), attempt['baseline'])
                self.assertEqual(self.tasks.read_bytes(), attempt['tasks_bytes'])
                self.assertEqual('implement', attempt['entry_mode'])
            self.assertEqual(1, len(list((self.root/'.symphony/single-task/contexts').iterdir())))
        return response

    def exercise(self, route, expected_code):
        if route == 'entry':
            return self.deferred(expected_code)
        module_name, function = {
            'schema': ('single_task_context', 'validate_context'),
            'json': ('single_task_context', 'load_context'),
            'scope': ('single_task_scope', 'validate_entry_scope'),
            'artifacts': ('single_task_context', 'validate_reviewed_artifacts'),
            'dependencies': ('single_task_context', 'validate_prerequisites'),
        }[route]
        module = self.module(module_name)
        if module is None or not callable(getattr(module, function, None)):
            return self.deferred(expected_code)
        before = self.snapshot()
        error = None
        try:
            if route == 'schema':
                result = module.validate_context(copy.deepcopy(self.context))
            elif route == 'json':
                self.input.write_bytes(self.raw)
                result = module.load_context(self.input)
            else:
                result = getattr(module, function)(self.root, copy.deepcopy(self.context))
        except ValueError as caught:
            error = caught
            result = None
        self.assertEqual(before, self.snapshot())
        self.assertFalse((self.root/'acceptance-ran').exists())
        if expected_code:
            self.assertIsNotNone(error, 'Invalid context was accepted')
            self.assertEqual(expected_code, getattr(error, 'code', None))
        else:
            self.assertIsNone(error, 'Valid context was rejected')
            if route == 'schema':
                self.assertEqual(self.context, result)
            elif route == 'scope':
                self.assertEqual({'T001', 'T002'}, set(result['tasks']))
                self.assertEqual(self.fixture.git('rev-parse', 'HEAD').stdout.strip(), result['baseline_commit'])
        return result

    def prepare(self, name):
        if name in {'valid_satisfied_dependency', 'unchecked_prerequisite',
                    'missing_prerequisite_evidence', 'duplicate_prerequisite_evidence',
                    'stale_prerequisite_commit', 'stale_prerequisite_digest',
                    'stale_prerequisite_check', 'failed_prerequisite_check', 'context_table_mismatch'}:
            self.dependency()
        if name == 'malformed_json':
            self.raw = b'{"schema_version":1,'
        elif name == 'missing_field':
            del self.context['task_id']
        elif name == 'extra_field':
            self.context['unknown'] = 'inert'
        elif name == 'duplicate_json_key':
            self.raw = canonical_bytes(self.context)[:-1] + b',"task_id":"T999"}'
        elif name == 'unsupported_schema':
            self.context['schema_version'] = 2
        elif name == 'multiple_task_ids':
            self.context['task_id'] = ['T001', 'T002']
        elif name in {'unknown_task_id', 'fenced_pseudo_task'}:
            if name == 'fenced_pseudo_task':
                self.tasks.write_bytes(self.tasks.read_bytes() + b'\n```markdown\n- [ ] T999 Pseudo task\n```\n')
                self.review_current()
            self.context['task_id'] = 'T999'
        elif name == 'duplicate_task_id':
            self.replace_tasks('- [ ] T002', '- [ ] T001 Duplicate\n- [ ] T002')
        elif name == 'fenced_duplicate_ignored':
            self.tasks.write_bytes(self.tasks.read_bytes() + b'\n~~~markdown\n- [ ] T001 Pseudo duplicate\n' + self.fixture.dependency_table.encode() + b'\n~~~\n')
            self.review_current()
        elif name == 'unknown_feature':
            self.context['feature'] = 'nonexistent'
            for artifact, record in self.context['reviewed_artifacts'].items():
                record['path'] = f'specs/nonexistent/{artifact}'
            self.context['source_assertion']['path'] = 'specs/nonexistent/tasks.md'
        elif name == 'dirty_tracked':
            (self.root/'guide.md').write_text('Uncommitted\n')
        elif name == 'dirty_index':
            (self.root/'guide.md').write_text('Staged\n')
            self.fixture.git('add', 'guide.md')
            (self.root/'guide.md').write_text('Before\n')
        elif name == 'dirty_untracked':
            (self.root/'untracked.md').write_text('Keep\n')
        elif name == 'missing_artifact':
            (self.fixture.feature/'plan.md').unlink()
            self.commit()
        elif name == 'unknown_artifact_commit':
            self.context['reviewed_artifacts']['spec.md']['commit'] = '0'*40
        elif name == 'nonancestor_artifact_commit':
            tree = self.fixture.git('rev-parse', 'HEAD^{tree}').stdout.strip()
            orphan = subprocess.run(['git', '-c', f'core.hooksPath={self.fixture.hooks}',
                                     '-c', 'commit.gpgSign=false', 'commit-tree', tree],
                                    input='Synthetic unrelated reviewed root\n', cwd=self.root,
                                    env=self.fixture.env, capture_output=True, text=True,
                                    check=True, timeout=10).stdout.strip()
            self.context['reviewed_artifacts']['spec.md']['commit'] = orphan
        elif name == 'artifact_digest_mismatch':
            self.context['reviewed_artifacts']['spec.md']['sha256'] = '0'*64
        elif name == 'missing_approval':
            del self.context['approval']
        elif name == 'changed_approval_binding':
            self.context['allowed_paths'] = ['other.md']
            return
        elif name in TABLE_MUTATIONS:
            before, after = TABLE_MUTATIONS[name]
            self.replace_tasks(before, after)
        elif name == 'context_table_mismatch':
            self.context['dependencies'] = []
            self.context['prerequisite_evidence'] = []
        elif name == 'source_digest_mismatch':
            self.context['source_assertion']['sha256'] = '0'*64
        elif name == 'source_excerpt_mismatch':
            self.context['source_assertion']['excerpt'] += '\n'
        elif name == 'unchecked_prerequisite':
            self.replace_tasks('[x] T001', '[ ] T001', reviewed=False)
        elif name == 'missing_prerequisite_evidence':
            self.context['prerequisite_evidence'] = []
        elif name == 'extra_prerequisite_evidence':
            record = self.fixture.complete_prerequisite()
            self.replace_tasks('[x] T001', '[ ] T001', reviewed=False)
            self.context['prerequisite_evidence'] = [record]
        elif name == 'duplicate_prerequisite_evidence':
            self.context['prerequisite_evidence'] *= 2
        elif name == 'stale_prerequisite_commit':
            self.context['prerequisite_evidence'][0]['implementation_commit'] = self.fixture.baseline
        elif name == 'stale_prerequisite_digest':
            self.context['prerequisite_evidence'][0]['content_manifest']['entries'][0]['sha256'] = '0'*64
        elif name == 'stale_prerequisite_check':
            self.context['prerequisite_evidence'][0]['check_results'][0]['content_manifest_digest'] = '0'*64
        elif name == 'failed_prerequisite_check':
            self.context['prerequisite_evidence'][0]['check_results'][0]['exit'] = 1
        elif name == 'progress_checkbox_only':
            self.dependency()
        elif name == 'progress_evidence_footer':
            self.tasks.write_text(self.tasks.read_text() + self.footer())
            self.commit()
        elif name in {'semantic_spec_drift', 'semantic_plan_drift'}:
            path = self.fixture.feature/('spec.md' if 'spec' in name else 'plan.md')
            path.write_bytes(path.read_bytes() + b'Changed requirements\n')
            self.commit()
        elif name == 'semantic_task_description':
            self.replace_tasks('Write the synthetic guide', 'Change approved requirements', reviewed=False)
        elif name == 'semantic_task_order':
            lines = self.tasks.read_text().splitlines(keepends=True)
            indices = [i for i, line in enumerate(lines) if line.startswith('- [')]
            lines[indices[0]], lines[indices[1]] = lines[indices[1]], lines[indices[0]]
            self.tasks.write_text(''.join(lines))
            self.commit()
        elif name in {'changed_existing_evidence', 'removed_existing_evidence'}:
            original = self.tasks.read_text()
            self.tasks.write_text(original + self.footer())
            self.review_current()
            if name == 'removed_existing_evidence':
                self.tasks.write_text(original)
            else:
                (self.root/'evidence.md').write_text('New evidence\n')
                self.tasks.write_text(original + self.footer().replace('guide.md ' + digest((self.root/'guide.md').read_bytes()), 'evidence.md ' + digest((self.root/'evidence.md').read_bytes())))
            self.commit()
        elif name == 'invalid_evidence_footer':
            self.tasks.write_text(self.tasks.read_text() + self.footer() + 'Unapproved prose\n')
            self.commit()
        self.seal()


TABLE_MUTATIONS = {
    'missing_dependency_table': ('## Dependencies\n\n| Task | Prerequisites |\n| --- | --- |\n| T001 | none |\n| T002 | T001 |', '## Dependencies\n\nNo authoritative table'),
    'malformed_dependency_table': ('| --- | --- |', '| -- | --- |'),
    'omitted_dependency_row': ('| T002 | T001 |', ''),
    'extra_dependency_row': ('| T002 | T001 |', '| T002 | T001 |\n| T999 | none |'),
    'duplicate_dependency_row': ('| T001 | none |', '| T001 | none |\n| T001 | none |'),
    'duplicate_dependency_table': ('| T002 | T001 |', '| T002 | T001 |\n\n| Task | Prerequisites |\n| --- | --- |\n| T001 | none |\n| T002 | T001 |'),
    'unknown_dependency_id': ('| T001 | none |', '| T001 | T999 |'),
    'self_dependency': ('| T001 | none |', '| T001 | T001 |'),
    'dependency_cycle': ('| T001 | none |', '| T001 | T002 |'),
    'contradictory_dependency_rows': ('| T002 | T001 |', '| T002 | T001 |\n| T002 | none |'),
    'unsorted_dependency_rows': ('| T001 | none |\n| T002 | T001 |', '| T002 | T001 |\n| T001 | none |'),
    'duplicate_dependency_ids': ('| T002 | T001 |', '| T002 | T001, T001 |'),
}

SCHEMA_NAMES = {'missing_field', 'extra_field', 'unsupported_schema', 'multiple_task_ids', 'missing_approval'}
SCOPE_NAMES = {'unknown_task_id', 'duplicate_task_id', 'fenced_pseudo_task', 'unknown_feature',
               'dirty_tracked', 'dirty_index', 'dirty_untracked', 'missing_artifact'}
ARTIFACT_NAMES = {'unknown_artifact_commit', 'nonancestor_artifact_commit', 'artifact_digest_mismatch',
                  'changed_approval_binding', 'progress_checkbox_only', 'progress_evidence_footer',
                  'semantic_spec_drift', 'semantic_plan_drift', 'semantic_task_description',
                  'semantic_task_order', 'changed_existing_evidence', 'removed_existing_evidence',
                  'invalid_evidence_footer'}
ENTRY_NAMES = {'valid_empty_dependencies', 'valid_satisfied_dependency'}


def scenario(name):
    if name in ENTRY_NAMES:
        return 'entry', None, 'T010'
    if name in {'malformed_json', 'duplicate_json_key'}:
        return 'json', 'invalid_json', 'T006'
    if name in SCHEMA_NAMES:
        return 'schema', 'context_schema', 'T006'
    if name == 'fenced_duplicate_ignored':
        return 'scope', None, 'T007'
    if name in SCOPE_NAMES:
        code = 'dirty_baseline' if name.startswith('dirty_') else 'scope_context'
        return 'scope', code, 'T007'
    if name in ARTIFACT_NAMES:
        code = ('artifact_commit' if name.endswith('artifact_commit') else
                'artifact_digest' if name == 'artifact_digest_mismatch' else
                'approval_digest' if name == 'changed_approval_binding' else
                None if name.startswith('progress_') else 'artifact_drift')
        return 'artifacts', code, 'T008'
    code = 'dependency_table' if name in TABLE_MUTATIONS else (
        'dependency_mismatch' if name in {'context_table_mismatch', 'source_digest_mismatch', 'source_excerpt_mismatch'}
        else 'prerequisite_evidence')
    return 'dependencies', code, 'T009'


def make_case(name):
    route, code, successor = scenario(name)
    def test(self):
        self.prepare(name)
        self.exercise(route, code)
    test.__name__ = 'test_' + name
    test.__doc__ = f'{successor}: {name}; {route}; expected {code or "accepted"}.'
    return test


# Keep all 54 reviewed proposal intentions visible; discovery still owns exact IDs.
_PROPOSAL = json.loads((ROOT/'docs/development/bootstrap-t005.json').read_bytes())
for _name, _mutation, _exit, _successors in _PROPOSAL['cases']:
    setattr(TaskContextEntryTests, 'test_' + _name, make_case(_name))




def schema_valid(self):
    self.exercise('schema', None)


def canonical_vector(self):
    module = self.module('single_task_context')
    if module is None or not callable(getattr(module, 'context_digest', None)):
        self.deferred(None)
        return
    value = {'z': 'é', 'approval': {'context_digest': 'excluded', 'owner_ref': 'owner', 'evidence_ref': 'ref'}}
    expected = b'{"approval":{"evidence_ref":"ref","owner_ref":"owner"},"z":"\xc3\xa9"}'
    self.assertEqual(expected, module.canonical_bytes({'approval': {'owner_ref': 'owner', 'evidence_ref': 'ref'}, 'z': 'é'}))
    self.assertEqual('1221bb7ae10deced6a45aceca4f24b54dfe28eaf89d3ba36ffeae7df73b19767', module.context_digest(value))
    self.assertEqual('excluded', value['approval']['context_digest'])
    for invalid in (float('nan'), float('inf'), float('-inf')):
        with self.assertRaises(ValueError):
            module.canonical_bytes({'number': invalid})


def schema_nested(self):
    original = copy.deepcopy(self.context)
    for path in [('approval',), ('source_assertion',), ('reviewed_artifacts', 'spec.md'), ('checks', 0)]:
        for field in ('unknown', None):
            self.context = copy.deepcopy(original)
            node = self.context
            for part in path:
                node = node[part]
            if field:
                node[field] = 'inert'
            else:
                del node[next(iter(node))]
            self.exercise('schema', 'context_schema')


def schema_primitives(self):
    original = copy.deepcopy(self.context)
    for field, values in {'schema_version': [True, 1.0, '1'], 'feature': ['', '../synthetic', ['synthetic']],
                          'task_id': ['', 'T01', 'T001,T002'], 'mode': ['handoff-only', 'rework'],
                          'dependencies': [None, ['T001', 'T001']],
                          'semantic_constraints': [[], [''], 'description']}.items():
        for value in values:
            self.context = copy.deepcopy(original)
            self.context[field] = value
            self.exercise('schema', 'context_schema')
    for field in ('owner_ref', 'evidence_ref'):
        self.context = copy.deepcopy(original)
        self.context['approval'][field] = ''
        self.exercise('schema', 'context_schema')


def schema_checks(self):
    original = copy.deepcopy(self.context)
    for key, values in {'id': ['', 1], 'argv': [[], [''], 'git diff'], 'cwd': ['', '../outside', '/tmp'],
                        'timeout_seconds': [True, 0, -1, float('nan'), float('inf')],
                        'expected_exit': [True, 1, 0.0]}.items():
        for value in values:
            self.context = copy.deepcopy(original)
            self.context['checks'][0][key] = value
            self.exercise('schema', 'context_schema')
    self.context = copy.deepcopy(original)
    self.context['checks'] *= 2
    self.exercise('schema', 'context_schema')


def schema_paths(self):
    original = copy.deepcopy(self.context)
    for paths in ([], ['guide.md', 'guide.md'], ['../outside'], ['/tmp/file'], ['a//b'], ['./guide.md'],
                  ['a/../b'], ['a\\b'], ['*.md'], ['folder/'], ['.git/config'],
                  ['.symphony/single-task/lock'], ['specs/synthetic/spec.md'],
                  ['specs/synthetic/plan.md'], ['specs/synthetic/tasks.md']):
        self.context = copy.deepcopy(original)
        self.context['allowed_paths'] = paths
        self.exercise('schema', 'context_schema')


def schema_rework(self):
    original = copy.deepcopy(self.context)
    for field in ('prior_attempt_id', 'prior_implementation_commit', 'rework_approval'):
        self.context = copy.deepcopy(original)
        self.context[field] = 'not permitted in implement'
        self.exercise('schema', 'context_schema')
    self.context = copy.deepcopy(original)
    self.context['mode'] = 'owner-authorized rework'
    self.exercise('schema', 'context_schema')


def scope_traversal(self):
    for feature in ('../synthetic', '/tmp/synthetic', 'synthetic/child', '.', 'synthetic/../synthetic'):
        self.context['feature'] = feature
        self.exercise('scope', 'scope_context')


def scope_symlink(self):
    outside = self.fixture.private/'external.md'
    outside.write_bytes(b'External content stays unchanged\n')
    artifact = self.fixture.feature/'plan.md'
    artifact.unlink()
    artifact.symlink_to(outside)
    self.commit()
    self.exercise('scope', 'scope_context')
    self.assertEqual(b'External content stays unchanged\n', outside.read_bytes())


def scope_nonregular(self):
    artifact = self.fixture.feature/'plan.md'
    artifact.unlink()
    artifact.mkdir()
    self.commit()
    self.exercise('scope', 'scope_context')


def scope_allowed_directory(self):
    self.context['allowed_paths'] = ['specs']
    self.seal()
    self.exercise('scope', 'scope_context')


def scope_allowed_symlink_component(self):
    outside = self.fixture.private/'outside'
    outside.mkdir()
    (self.root/'linked').symlink_to(outside, target_is_directory=True)
    self.commit()
    self.context['allowed_paths'] = ['linked/new.md']
    self.seal()
    self.exercise('scope', 'scope_context')
    self.assertEqual([], list(outside.iterdir()))


def scope_checked_implement(self):
    self.replace_tasks('[ ] T001', '[x] T001')
    self.exercise('scope', 'scope_context')


def scope_fence_lengths(self):
    self.tasks.write_bytes(self.tasks.read_bytes() + b'\n````markdown\n```\n- [ ] T001 Pseudo\n````\n')
    self.review_current()
    self.exercise('scope', None)


def artifacts_fenced_checkbox(self):
    self.tasks.write_bytes(self.tasks.read_bytes() + b'\n```markdown\n- [ ] T999 Example\n```\n')
    self.review_current()
    self.replace_tasks('[ ] T999', '[x] T999', reviewed=False)
    self.exercise('artifacts', 'artifact_drift')


def artifacts_footer_records(self):
    original = self.tasks.read_bytes()
    footer = self.footer()
    variants = [footer.replace('T001', 'T999'), footer.replace('guide.md', '../guide.md'),
                footer.replace(digest((self.root/'guide.md').read_bytes()), '0'*64),
                footer.replace(digest((self.root/'guide.md').read_bytes()), 'A'*64),
                footer.replace('guide.md', 'absent.md'), footer + '\nUnexpected prose\n']
    for variant in variants:
        self.tasks.write_bytes(original + variant.encode())
        self.commit()
        self.exercise('artifacts', 'artifact_drift')


def artifacts_spec_plan_footer(self):
    for name in ('spec.md', 'plan.md'):
        artifact = self.fixture.feature/name
        artifact.write_bytes(artifact.read_bytes() + self.footer().encode())
        self.commit()
    self.exercise('artifacts', None)


def artifacts_binding_fields(self):
    original = copy.deepcopy(self.context)
    for key, value in [('task_id', 'T002'),
                       ('semantic_constraints', ['Changed scope']), ('dependencies', ['T001']),
                       ('checks', [dict(original['checks'][0], argv=['git', 'status'])])]:
        self.context = copy.deepcopy(original)
        self.context[key] = value
        self.exercise('artifacts', 'approval_digest')


def artifacts_rework_binding(self):
    self.context.update(mode='owner-authorized rework', prior_attempt_id='synthetic-prior-attempt',
                        prior_implementation_commit=self.fixture.baseline,
                        rework_approval={'owner_ref': self.fixture.identities['owner'],
                                        'evidence_ref': 'synthetic:explicit-corrective-approval'})
    self.exercise('artifacts', 'approval_digest')


def artifacts_append_existing_footer(self):
    self.tasks.write_bytes(self.tasks.read_bytes() + self.footer().encode())
    self.review_current()
    record = self.footer().splitlines()[-1].encode() + b'\n'
    self.tasks.write_bytes(self.tasks.read_bytes() + record)
    self.commit()
    self.exercise('artifacts', None)


def artifacts_crlf_bytes(self):
    self.tasks.write_bytes(self.tasks.read_bytes().replace(b'\n', b'\r\n'))
    self.review_current()
    self.replace_tasks('[ ] T002', '[x] T002', reviewed=False)
    self.exercise('artifacts', None)
    self.tasks.write_bytes(self.tasks.read_bytes().replace(b'\r\n', b'\n'))
    self.commit()
    self.exercise('artifacts', 'artifact_drift')


def artifacts_fenced_footer(self):
    self.tasks.write_bytes(self.tasks.read_bytes() + b'\n```markdown\n')
    self.review_current()
    self.tasks.write_bytes(self.tasks.read_bytes() + self.footer().encode())
    self.commit()
    self.exercise('artifacts', 'artifact_drift')


def prerequisites_valid(self):
    self.assertEqual({}, self.exercise('dependencies', None))
    self.dependency()
    self.assertEqual({'T001'}, set(self.exercise('dependencies', None)))


def prerequisites_fenced_crlf(self):
    self.tasks.write_bytes(self.tasks.read_bytes() + b'\n~~~~markdown\n' +
                          self.fixture.dependency_table.encode() + b'\n~~~~\n')
    self.tasks.write_bytes(self.tasks.read_bytes().replace(b'\n', b'\r\n'))
    self.review_current()
    self.assertEqual({}, self.exercise('dependencies', None))
    self.replace_tasks('| T001 | none |', '```markdown\nUnrelated example\n```\n| T001 | none |')
    self.exercise('dependencies', 'dependency_table')


def dependency_sorted_lists(self):
    self.tasks.write_bytes(self.tasks.read_bytes().replace(b'\n## Dependencies',
                          b'\n- [ ] T003 Third task\n\n## Dependencies') + b'| T003 | T001, T002 |\n')
    self.review_current()
    self.assertEqual({}, self.exercise('dependencies', None))
    original = self.tasks.read_bytes()
    for value in (b'T002, T001', b'none, T001', b'T001,T002', b'T001; T002'):
        self.tasks.write_bytes(original.replace(b'T001, T002', value))
        self.review_current()
        self.exercise('dependencies', 'dependency_table')


def prerequisite_manifest_types(self):
    self.dependency()
    original = copy.deepcopy(self.context['prerequisite_evidence'][0])
    for replacement in ({'path': 'guide.md', 'type': 'regular', 'mode': '100755',
                         'sha256': original['content_manifest']['entries'][0]['sha256']},
                        {'path': 'guide.md', 'type': 'absent', 'mode': None, 'sha256': None}):
        record = copy.deepcopy(original)
        record['content_manifest']['entries'][0] = replacement
        record['check_results'][0]['content_manifest_digest'] = digest(canonical_bytes(record['content_manifest']))
        self.context['prerequisite_evidence'] = [record]
        self.seal()
        self.exercise('dependencies', 'prerequisite_evidence')


def dependency_bare_cr(self):
    self.tasks.write_bytes(self.tasks.read_bytes().replace(b'\n', b'\r'))
    self.review_current()
    self.exercise('dependencies', 'dependency_table')


def entry_status_readonly(self):
    begun = self.deferred(None)
    before = self.snapshot()
    result = subprocess.run([sys.executable, '-B', str(ROOT/'scripts/single_task.py'),
                             'status', '--attempt', begun['attempt_id']], cwd=self.root,
                            env=self.fixture.env, capture_output=True, text=True, timeout=10)
    self.assertEqual(0, result.returncode, result.stderr)
    self.assertEqual(begun, json.loads(result.stdout))
    self.assertEqual(before, self.snapshot())


def entry_rejections_preserve_evidence(self):
    original = copy.deepcopy(self.context)
    for field, value, seal, code in [('unknown', 'inert', True, 'context_schema'),
                                     ('task_id', 'T999', True, 'scope_context')]:
        self.context = copy.deepcopy(original)
        self.context[field] = value
        if seal:
            self.seal()
        self.deferred(code)
    self.context = copy.deepcopy(original)
    self.context['approval']['context_digest'] = '0'*64
    self.deferred('approval_digest')
    self.context = copy.deepcopy(original)
    self.context['source_assertion']['excerpt'] += '\n'
    self.seal()
    self.deferred('dependency_mismatch')


def entry_legacy_rejected(self):
    before = self.snapshot()
    result = subprocess.run([sys.executable, '-B', str(ROOT/'scripts/single_task.py'),
                             'begin', '--feature', 'synthetic', '--task', 'T001',
                             '--allow', 'guide.md', '--check', '["true"]'], cwd=self.root,
                            env=self.fixture.env, capture_output=True, text=True, timeout=10)
    self.assertEqual(2, result.returncode)
    self.assertEqual('invalid_arguments', json.loads(result.stdout)['errors'][0]['code'])
    self.assertEqual('', result.stderr)
    self.assertEqual(before, self.snapshot())


def entry_event_rollback(self):
    self.input.write_bytes(canonical_bytes(self.context))
    before = self.snapshot(include_state=False)
    injection = '''import pathlib,runpy,sys
sys.path.insert(0,str(pathlib.Path(sys.argv[1]).parent))
import single_task_state as backend
original=backend._Store.append_event
def failed(self,attempt_id,kind,payload,progress=None):
    if kind=='entry': raise backend.PersistenceFailure('synthetic entry event failure')
    return original(self,attempt_id,kind,payload,progress)
backend._Store.append_event=failed
sys.argv=sys.argv[1:]
runpy.run_path(sys.argv[0],run_name='__main__')
'''
    result = subprocess.run([sys.executable, '-B', '-c', injection,
                             str(ROOT/'scripts/single_task.py'), 'begin', '--context', str(self.input)],
                            cwd=self.root, env=self.fixture.env, capture_output=True, text=True, timeout=10)
    self.assertEqual(6, result.returncode)
    self.assertEqual('persistence_failure', json.loads(result.stdout)['errors'][0]['code'])
    self.assertEqual('', result.stderr)
    self.assertEqual(before, self.snapshot(include_state=False))
    journal = self.root/'.symphony/single-task/journal.sqlite3'
    with sqlite3.connect(journal.as_uri() + '?mode=ro', uri=True) as connection:
        tables = connection.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()
        for (name,) in tables:
            quoted = '"' + name.replace('"', '""') + '"'
            self.assertEqual([], connection.execute('SELECT * FROM ' + quoted).fetchall())
    self.assertEqual(1, len(list((self.root/'.symphony/single-task/contexts').iterdir())))
    self.assertEqual(b'Retain prior local evidence\n', (self.root/'.symphony/prior-evidence').read_bytes())


def schema_valid_rework(self):
    self.context.update(mode='owner-authorized rework', prior_attempt_id='synthetic-prior-attempt',
                        prior_implementation_commit=self.fixture.baseline,
                        rework_approval={'owner_ref': self.fixture.identities['owner'],
                                        'evidence_ref': 'synthetic:explicit-corrective-approval'})
    self.seal()
    self.exercise('schema', None)


def schema_unicode_feature(self):
    self.context['feature'] = '_équipe'
    for name, record in self.context['reviewed_artifacts'].items():
        record['path'] = f'specs/_équipe/{name}'
    self.context['source_assertion']['path'] = 'specs/_équipe/tasks.md'
    self.seal()
    self.exercise('schema', None)


def scope_unicode_feature(self):
    renamed = self.root/'specs/_équipe'
    self.fixture.feature.rename(renamed)
    self.fixture.feature = renamed
    self.tasks = renamed/'tasks.md'
    self.context['feature'] = '_équipe'
    self.context['source_assertion']['path'] = 'specs/_équipe/tasks.md'
    self.review_current()
    self.exercise('scope', None)


def scope_feature_symlink(self):
    outside = self.fixture.private/'external-feature'
    self.fixture.feature.rename(outside)
    self.fixture.feature.symlink_to(outside, target_is_directory=True)
    self.commit()
    self.exercise('scope', 'scope_context')


def scope_specs_symlink(self):
    outside = self.fixture.private/'external-specs'
    (self.root/'specs').rename(outside)
    (self.root/'specs').symlink_to(outside, target_is_directory=True)
    self.commit()
    self.exercise('scope', 'scope_context')


def scope_check_cwd_symlink(self):
    (self.root/'linked').symlink_to(self.fixture.private, target_is_directory=True)
    self.commit()
    self.context['checks'][0]['cwd'] = 'linked'
    self.seal()
    self.exercise('scope', 'scope_context')


def scope_hidden_tracked(self):
    subprocess.run(['git', 'update-index', '--assume-unchanged', '--', 'guide.md'],
                   cwd=self.root, env=self.fixture.env, check=True, capture_output=True, timeout=10)
    (self.root/'guide.md').write_text('Hidden modified bytes\n')
    self.assertEqual('', self.fixture.git('status', '--porcelain').stdout)
    self.exercise('scope', 'dirty_baseline')


def scope_hidden_mode(self):
    self.fixture.git('config', 'core.filemode', 'false')
    path = self.root/'guide.md'
    path.chmod(path.stat().st_mode | 0o111)
    self.assertEqual('', self.fixture.git('status', '--porcelain').stdout)
    self.exercise('scope', 'dirty_baseline')


def scope_git_hooks(self):
    sentinel = self.fixture.private/'fsmonitor-ran'
    hook = self.fixture.private/'fsmonitor'
    hook.write_text('#!/bin/sh\ntouch "' + str(sentinel) + '"\nprintf "2\\0"\n')
    hook.chmod(0o700)
    self.fixture.git('config', 'core.fsmonitor', str(hook))
    self.exercise('scope', None)
    self.assertFalse(sentinel.exists())


def scope_executable_filter(self):
    (self.root/'.gitattributes').write_text('guide.md filter=probe\n')
    self.commit()
    sentinel = self.fixture.private/'filter-ran'
    self.fixture.git('config', 'filter.probe.clean', 'touch "' + str(sentinel) + '"; cat')
    self.exercise('scope', 'scope_context')
    self.assertFalse(sentinel.exists())


def scope_alias_paths(self):
    original = self.context['allowed_paths']
    for path in ('.GIT/config', '.SYMPHONY/single-task/lock', 'specs/SYNTHETIC/tasks.md'):
        self.context['allowed_paths'] = [path]
        self.seal()
        self.exercise('scope', 'scope_context')
    self.context['allowed_paths'] = original


def gitlink_fixture(self):
    directory = self.fixture.private/'submodule-fixture'
    directory.mkdir()
    submodule = SingleTaskFixture(directory)
    destination = self.root/'vendor'
    submodule.root.rename(destination)
    submodule.root = destination
    subprocess.run(['git', 'update-index', '--add', '--cacheinfo',
                    f'160000,{submodule.baseline},vendor'], cwd=self.root,
                   env=self.fixture.env, check=True, capture_output=True, timeout=10)
    self.fixture.git('commit', '-m', 'test: retain baseline submodule')
    return submodule


def scope_clean_gitlink(self):
    submodule = gitlink_fixture(self)
    self.assertEqual('', self.fixture.git('status', '--porcelain').stdout)
    self.exercise('scope', None)
    submodule.root.rename(self.fixture.private/'uninitialized-retained')
    self.exercise('scope', None)
    (self.root/'vendor').mkdir()
    self.exercise('scope', None)


def scope_changed_gitlink(self):
    submodule = gitlink_fixture(self)
    path = submodule.root/'guide.md'
    path.write_bytes(b'Uncommitted submodule bytes\n')
    self.exercise('scope', 'dirty_baseline')
    submodule.git('add', '.')
    submodule.git('commit', '-m', 'test: changed submodule commit')
    self.exercise('scope', 'dirty_baseline')


def scope_gitlink_output(self):
    gitlink_fixture(self)
    self.context['allowed_paths'] = ['vendor/new.md']
    self.seal()
    self.exercise('scope', 'scope_context')
    self.context['allowed_paths'] = ['guide.md']
    self.context['checks'][0]['cwd'] = 'vendor'
    self.seal()
    self.exercise('scope', 'scope_context')


EXTRA_CASES = {
    'entry_event_rollback': (entry_event_rollback, 'entry', None, 'T010'),
    'entry_status_readonly': (entry_status_readonly, 'entry', None, 'T010'),
    'entry_rejections_preserve_evidence': (entry_rejections_preserve_evidence, 'entry', None, 'T010'),
    'entry_legacy_rejected': (entry_legacy_rejected, 'entry', None, 'T010'),
    'dependency_bare_cr': (dependency_bare_cr, 'dependencies', 'dependency_table', 'T009'),
    'prerequisites_valid': (prerequisites_valid, 'dependencies', None, 'T009'),
    'prerequisites_fenced_crlf': (prerequisites_fenced_crlf, 'dependencies', None, 'T009'),
    'dependency_sorted_lists': (dependency_sorted_lists, 'dependencies', None, 'T009'),
    'prerequisite_manifest_types': (prerequisite_manifest_types, 'dependencies', 'prerequisite_evidence', 'T009'),
    'artifacts_rework_binding': (artifacts_rework_binding, 'artifacts', 'approval_digest', 'T008'),
    'artifacts_append_existing_footer': (artifacts_append_existing_footer, 'artifacts', None, 'T008'),
    'artifacts_crlf_bytes': (artifacts_crlf_bytes, 'artifacts', None, 'T008'),
    'artifacts_fenced_footer': (artifacts_fenced_footer, 'artifacts', 'artifact_drift', 'T008'),
    'scope_clean_gitlink': (scope_clean_gitlink, 'scope', None, 'T007'),
    'scope_changed_gitlink': (scope_changed_gitlink, 'scope', 'dirty_baseline', 'T007'),
    'scope_gitlink_output': (scope_gitlink_output, 'scope', 'scope_context', 'T007'),
    'scope_unicode_feature': (scope_unicode_feature, 'scope', None, 'T007'),
    'scope_feature_symlink': (scope_feature_symlink, 'scope', 'scope_context', 'T007'),
    'scope_specs_symlink': (scope_specs_symlink, 'scope', 'scope_context', 'T007'),
    'scope_check_cwd_symlink': (scope_check_cwd_symlink, 'scope', 'scope_context', 'T007'),
    'scope_hidden_tracked': (scope_hidden_tracked, 'scope', 'dirty_baseline', 'T007'),
    'scope_hidden_mode': (scope_hidden_mode, 'scope', 'dirty_baseline', 'T007'),
    'scope_git_hooks': (scope_git_hooks, 'scope', None, 'T007'),
    'scope_executable_filter': (scope_executable_filter, 'scope', 'scope_context', 'T007'),
    'scope_alias_paths': (scope_alias_paths, 'scope', 'scope_context', 'T007'),
    'schema_unicode_feature': (schema_unicode_feature, 'schema', None, 'T006'),
    'schema_valid_rework': (schema_valid_rework, 'schema', None, 'T006'),
    'schema_valid': (schema_valid, 'schema', None, 'T006'),
    'canonical_vector': (canonical_vector, 'canonical', None, 'T006'),
    'schema_nested_fields': (schema_nested, 'schema', 'context_schema', 'T006'),
    'schema_primitive_types': (schema_primitives, 'schema', 'context_schema', 'T006'),
    'schema_checks': (schema_checks, 'schema', 'context_schema', 'T006'),
    'schema_allowed_paths': (schema_paths, 'schema', 'context_schema', 'T006'),
    'schema_rework_fields': (schema_rework, 'schema', 'context_schema', 'T006'),
    'scope_traversal': (scope_traversal, 'scope', 'scope_context', 'T007'),
    'scope_symlink_artifact': (scope_symlink, 'scope', 'scope_context', 'T007'),
    'scope_nonregular_artifact': (scope_nonregular, 'scope', 'scope_context', 'T007'),
    'scope_allowed_directory': (scope_allowed_directory, 'scope', 'scope_context', 'T007'),
    'scope_allowed_symlink_component': (scope_allowed_symlink_component, 'scope', 'scope_context', 'T007'),
    'scope_checked_implement': (scope_checked_implement, 'scope', 'scope_context', 'T007'),
    'scope_fence_lengths': (scope_fence_lengths, 'scope', None, 'T007'),
    'artifacts_fenced_checkbox': (artifacts_fenced_checkbox, 'artifacts', 'artifact_drift', 'T008'),
    'artifacts_footer_records': (artifacts_footer_records, 'artifacts', 'artifact_drift', 'T008'),
    'artifacts_spec_plan_footer': (artifacts_spec_plan_footer, 'artifacts', None, 'T008'),
    'artifacts_binding_fields': (artifacts_binding_fields, 'artifacts', 'approval_digest', 'T008'),
}
for _name, (_function, _route, _code, _successor) in EXTRA_CASES.items():
    setattr(TaskContextEntryTests, 'test_' + _name, _function)


class TaskContextReworkTests(unittest.TestCase):
    """T019 proposal: real predecessors, with future CLI gates in test bodies.

    No bound-commit event is synthesized. Cases needing a bound predecessor first
    call record-commit; until T023 exists their deeper T022 assertions are unreached.
    Synthetic approval references below are local fixture data, not authentication.
    """

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        self.tasks = self.fixture.feature/'tasks.md'
        (self.root/'.git/info').mkdir(exist_ok=True)
        with (self.root/'.git/info/exclude').open('a') as exclude:
            exclude.write('\n.symphony/\n')
        sys.path.insert(0, str(ROOT/'scripts'))
        self.addCleanup(sys.path.remove, str(ROOT/'scripts'))
        import single_task_state
        self.state = single_task_state
        wrong_task = self._testMethodName == 'test_wrong_predecessor_task'
        prerequisite = self.fixture.complete_prerequisite() if wrong_task else None
        self.prior_context = self.fixture.context('T002' if wrong_task else 'T001', prerequisite)
        entry_tasks = self.tasks.read_bytes()
        self.prior_checks = self.fixture.private/'prior-checks'
        self.corrective_checks = self.fixture.private/'corrective-checks'
        self.prior_context['checks'] = self.checks('prior', self.prior_checks)
        self.seal(self.prior_context)
        path = self.fixture.private/'prior-context.json'
        path.write_bytes(canonical_bytes(self.prior_context))
        run, response = self.cli('begin', '--context', str(path))
        self.assertEqual(0, run.returncode, 'Real implement entry is required fixture setup')
        self.prior_id = response['attempt_id']
        (self.root/self.prior_context['allowed_paths'][0]).write_bytes(b'Original implemented output\n')
        review = self.review(self.prior_id, self.prior_context)
        for command in ('verify', 'complete'):
            run, response = self.cli(command, '--attempt', self.prior_id, '--review', str(review))
            self.assertEqual(0, run.returncode, 'Real reviewed completion is required fixture setup')
        self.assertEqual('completed-awaiting-commit', response['progress'])
        self.assertEqual('handoff-only', response['mode'])
        task_id = self.prior_context['task_id'].encode('ascii')
        self.assertEqual(entry_tasks.replace(b'[ ] ' + task_id, b'[x] ' + task_id),
                         self.tasks.read_bytes())
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: actual completed predecessor')
        self.prior_commit = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        self.assertEqual('', self.fixture.git('status', '--porcelain').stdout)
        self.context = copy.deepcopy(self.prior_context)
        self.context.update(mode='owner-authorized rework', prior_attempt_id=self.prior_id,
                            prior_implementation_commit=self.prior_commit,
                            rework_approval={'owner_ref': self.fixture.identities['owner'],
                                             'evidence_ref': 'synthetic:corrective-authorization'})
        self.context['semantic_constraints'] = ['Correct only the synthetic guide output; retain checked tasks.']
        self.context['checks'] = self.checks('correction', self.corrective_checks)
        self.seal(self.context)
        self.capture_prior()

    def checks(self, name, counter):
        program = 'from pathlib import Path; p=Path(__import__("sys").argv[1]); p.write_bytes((p.read_bytes() if p.exists() else b"")+b"ran\\n")'
        return [{'id': name, 'argv': [sys.executable, '-B', '-c', program, str(counter)],
                 'cwd': '.', 'timeout_seconds': 5, 'expected_exit': 0}]

    def seal(self, context):
        context['approval']['context_digest'] = context_digest(context)

    def cli(self, *arguments):
        run = subprocess.run([sys.executable, '-B', str(ROOT/'scripts/single_task.py'), *arguments],
                             cwd=self.root, env=self.fixture.env, capture_output=True,
                             text=True, timeout=15)
        value = json.loads(run.stdout)
        self.assertEqual('', run.stderr)
        self.assertEqual(1, value['schema_version'])
        self.assertFalse(value['owner_identity_authenticated'])
        self.assertFalse(value['live_dispatch_verified'])
        return run, value

    def review(self, attempt_id, context):
        counts = [path.read_bytes() if path.exists() else None
                  for path in (self.prior_checks, self.corrective_checks)]
        run, inspection = self.cli('inspect', '--attempt', attempt_id)
        self.assertEqual(0, run.returncode, 'Actual inspect must prepare the review fixture')
        value = self.fixture.review(context)
        value.update(attempt_id=attempt_id,
                     content_manifest_digest=inspection['content_manifest_digest'])
        self.assertEqual(value['context_digest'], inspection['context_digest'])
        self.assertEqual(context['task_id'], inspection['task_id'])
        self.assertEqual(counts, [path.read_bytes() if path.exists() else None
                                 for path in (self.prior_checks, self.corrective_checks)])
        path = self.fixture.private/(attempt_id + '-review.json')
        path.write_bytes(canonical_bytes(value))
        return path

    def capture_prior(self):
        with self.state.open_store(self.root, require_existing=True) as store:
            self.prior_attempt = store.read_attempt(self.prior_id)
            self.prior_events = store.read_events(self.prior_id)
        self.prior_copies = {path.name: (path.stat().st_mode, path.read_bytes())
                             for path in (self.root/'.symphony/single-task/contexts').iterdir()}
        self.checked_bytes = self.tasks.read_bytes()

    def preserve_prior(self, *, include_progress=True):
        with self.state.open_store(self.root, require_existing=True) as store:
            current = store.read_attempt(self.prior_id)
            if include_progress:
                self.assertEqual(self.prior_attempt, current)
            else:
                self.assertEqual({key: value for key, value in self.prior_attempt.items() if key != 'progress'},
                                 {key: value for key, value in current.items() if key != 'progress'})
            self.assertEqual(self.prior_events, store.read_events(self.prior_id)[:len(self.prior_events)])
        for name, value in self.prior_copies.items():
            path = self.root/'.symphony/single-task/contexts'/name
            self.assertEqual(value, (path.stat().st_mode, path.read_bytes()))
        self.assertEqual(self.checked_bytes, self.tasks.read_bytes())

    def rework(self):
        path = self.fixture.private/'rework-context.json'
        path.write_bytes(canonical_bytes(self.context))
        self.rework_product = self.product()
        return self.cli('rework', '--context', str(path))

    def product(self):
        files = {}
        for path in self.root.rglob('*'):
            relative = path.relative_to(self.root).as_posix()
            if relative.startswith(('.git/', '.symphony/')):
                continue
            if path.is_file():
                files[relative] = (path.stat().st_mode, path.read_bytes())
        return files, (self.root/'.git/index').read_bytes(), self.fixture.git('rev-parse', 'HEAD').stdout

    def assert_blocked(self, run, response, exit_code, code):
        self.preserve_prior()
        self.assertEqual(self.rework_product, self.product())
        self.assertEqual(set(self.prior_copies),
                         {path.name for path in (self.root/'.symphony/single-task/contexts').iterdir()})
        self.assertFalse(self.corrective_checks.exists())
        self.assertEqual('not-run', response['local_verification'])
        self.assertEqual(exit_code, run.returncode, 'Rework must return its specific blocker category')
        self.assertEqual(code, response['errors'][0]['code'], 'Rework must validate the supplied corrective context')
        self.assertIsNone(response['mode'])
        self.assertNotEqual('handoff-ready', response['progress'])

    def bind_prior(self):
        return self.cli('record-commit', '--attempt', self.prior_id, '--commit', self.prior_commit)

    def bound(self, run, response):
        self.assertEqual([], response['errors'])
        self.assertEqual(self.prior_id, response['attempt_id'])
        self.preserve_prior(include_progress=False)
        review = self.review(self.prior_id, self.prior_context)
        run, response = self.cli('resume', '--attempt', self.prior_id, '--review', str(review))
        self.assertEqual(0, run.returncode, 'Real fresh resume capability is required before rework semantics')
        self.assertEqual('handoff-ready', response['progress'])
        self.assertEqual('handoff-only', response['mode'])
        self.preserve_prior(include_progress=False)
        self.capture_prior()

    def assert_linked(self, run, response):
        self.assertEqual(0, run.returncode, 'Authorized rework must create a linked attempt')
        self.assertEqual(self.rework_product, self.product())
        self.assertEqual('owner-authorized rework', response['mode'])
        self.assertEqual('active', response['progress'])
        self.assertEqual([], response['errors'])
        self.assertNotEqual(self.prior_id, response['attempt_id'])
        self.assertFalse(self.corrective_checks.exists())
        with self.state.open_store(self.root, require_existing=True) as store:
            current = store.read_attempt(response['attempt_id'])
            self.assertEqual(response['attempt_id'], store.active_attempt()['attempt_id'])
            self.assertEqual(self.context, current['context'])
            self.assertEqual(self.prior_id, current['predecessor'])
            self.assertEqual(self.prior_commit, current['baseline'])
            self.assertEqual(self.checked_bytes, current['tasks_bytes'])
            self.assertEqual('owner-authorized rework', current['entry_mode'])
        self.assertEqual(1, len({path.name for path in
                                (self.root/'.symphony/single-task/contexts').iterdir()} - set(self.prior_copies)))
        self.preserve_prior()
        return response['attempt_id']

    def test_missing_rework_approval(self):
        del self.context['rework_approval']
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 2, 'context_schema')

    def test_missing_prior_attempt(self):
        del self.context['prior_attempt_id']
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 2, 'context_schema')

    def test_missing_prior_implementation(self):
        del self.context['prior_implementation_commit']
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 2, 'context_schema')

    def test_unrenewed_corrective_scope(self):
        self.context['allowed_paths'] = ['correction.md']
        self.assert_blocked(*self.rework(), 2, 'approval_digest')

    def test_unrenewed_corrective_checks(self):
        self.context['checks'][0]['id'] = 'unapproved-replacement'
        self.assert_blocked(*self.rework(), 2, 'approval_digest')

    def test_retry_counter_cannot_authorize_rework(self):
        del self.context['rework_approval']
        self.context['retry_count'] = 2
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 2, 'context_schema')

    def test_ready_assertion_cannot_authorize_rework(self):
        del self.context['rework_approval']
        self.context['ready'] = True
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 2, 'context_schema')

    def test_absent_retained_predecessor(self):
        self.context['prior_attempt_id'] = 'synthetic-absent-attempt'
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 3, 'state_blocker')

    def test_unbound_prior_implementation_cannot_authorize_rework(self):
        self.assertEqual('completed-awaiting-commit', self.prior_attempt['progress'])
        self.assertEqual(self.prior_commit, self.fixture.git('rev-parse', 'HEAD').stdout.strip())
        self.assert_blocked(*self.rework(), 3, 'state_blocker')

    def test_wrong_predecessor_task(self):
        run, response = self.bind_prior()
        self.assertEqual(0, run.returncode, 'Real record-commit capability is required before rework semantics')
        self.bound(run, response)
        # Both tasks are checked: a real T002 predecessor cannot authorize T001.
        replacement = self.fixture.context('T001')
        replacement.update({key: copy.deepcopy(self.context[key]) for key in
                            ('mode', 'prior_attempt_id', 'prior_implementation_commit', 'rework_approval',
                             'checks', 'semantic_constraints')})
        self.context = replacement
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 3, 'state_blocker')

    def test_wrong_prior_implementation_commit(self):
        run, response = self.bind_prior()
        self.assertEqual(0, run.returncode, 'Real record-commit capability is required before rework semantics')
        self.bound(run, response)
        self.context['prior_implementation_commit'] = self.fixture.baseline
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 3, 'state_blocker')

    def test_unavailable_prior_implementation_commit(self):
        run, response = self.bind_prior()
        self.assertEqual(0, run.returncode, 'Real record-commit capability is required before rework semantics')
        self.bound(run, response)
        self.context['prior_implementation_commit'] = '0' * 40
        self.seal(self.context)
        self.assert_blocked(*self.rework(), 3, 'state_blocker')

    def test_semantic_artifact_drift_requires_renewal(self):
        run, response = self.bind_prior()
        self.assertEqual(0, run.returncode, 'Real record-commit capability is required before rework semantics')
        self.bound(run, response)
        (self.fixture.feature/'plan.md').write_bytes(b'Changed corrective requirements\n')
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: semantic plan drift')
        self.assert_blocked(*self.rework(), 2, 'artifact_drift')

    def test_dirty_rework_baseline_preserves_work(self):
        run, response = self.bind_prior()
        self.assertEqual(0, run.returncode, 'Real record-commit capability is required before rework semantics')
        self.bound(run, response)
        (self.root/'guide.md').write_bytes(b'Uncommitted correction must be retained\n')
        self.assert_blocked(*self.rework(), 2, 'dirty_baseline')
        self.assertEqual(b'Uncommitted correction must be retained\n', (self.root/'guide.md').read_bytes())

    def test_authorized_entry_retains_marker_and_prior_evidence(self):
        run, response = self.bind_prior()
        self.assertEqual(0, run.returncode, 'Real record-commit capability is required before rework semantics')
        self.bound(run, response)
        self.assert_linked(*self.rework())

    def test_corrective_completion_retains_checked_bytes_and_old_history(self):
        run, response = self.bind_prior()
        self.assertEqual(0, run.returncode, 'Real record-commit capability is required before rework semantics')
        self.bound(run, response)
        attempt_id = self.assert_linked(*self.rework())
        (self.root/'guide.md').write_bytes(b'Explicitly authorized corrective output\n')
        review = self.review(attempt_id, self.context)
        for command in ('verify', 'complete'):
            run, response = self.cli(command, '--attempt', attempt_id, '--review', str(review))
            self.assertEqual(0, run.returncode, 'Current reviewed corrective acceptance must pass')
        self.assertEqual(b'ran\nran\n', self.corrective_checks.read_bytes())
        self.assertEqual('handoff-only', response['mode'])
        self.assertEqual('completed-awaiting-commit', response['progress'])
        with self.state.open_store(self.root, require_existing=True) as store:
            intent = [event['payload'] for event in store.read_events(attempt_id)
                      if event['kind'] == 'completion-intent']
            self.assertEqual(1, len(intent))
            self.assertEqual(self.checked_bytes, bytes.fromhex(intent[0]['tasks_before_hex']))
            self.assertEqual(self.checked_bytes, bytes.fromhex(intent[0]['tasks_after_hex']))
        self.preserve_prior()

    def test_renewed_corrective_scope_and_checks_are_bound(self):
        run, response = self.bind_prior()
        self.assertEqual(0, run.returncode, 'Real record-commit capability is required before rework semantics')
        self.bound(run, response)
        self.context['allowed_paths'] = ['correction.md']
        self.context['semantic_constraints'] = ['Write only correction.md; preserve old implementation and tasks.']
        self.context['checks'][0]['id'] = 'renewed-corrective-check'
        self.context['rework_approval']['evidence_ref'] = 'synthetic:renewed-corrective-scope-and-checks'
        self.seal(self.context)
        self.assert_linked(*self.rework())


if __name__ == '__main__':
    unittest.main()
