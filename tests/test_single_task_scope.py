"""Proposed T011 owning scope contracts; not approved task acceptance.

Every case begins a real validated T010 attempt before observing its boundary.
Missing owning APIs are explicit test-body assertions, never import/setup errors.
The exact observed matrix and the proposed API signatures require owner review.
"""
import copy
import importlib.util
import json
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from test_single_task import SingleTaskFixture, canonical_bytes, context_digest, digest

ROOT = Path(__file__).resolve().parents[1]
TASKS = 'specs/synthetic/tasks.md'
VECTOR_TASKS = b'- [ ] T001 Scope\r\n- [ ] T002 Later\r\n'
VECTOR_BYTES = ('{"entries":[{"mode":"100644","path":"á.txt","sha256":"a95c16aa568fee306afb42ea729e98485a3eac4d970e755f829ba7140f17d024","type":"regular"},'
                '{"mode":null,"path":"missing.txt","sha256":null,"type":"absent"},'
                '{"mode":"100644","path":"specs/synthetic/tasks.md","sha256":"fdc2587b398411f49f470266e66d59235464a2fbfa6ab66860b6545e5b4f2d5b","type":"regular"},'
                '{"mode":"100644","path":"é.txt","sha256":"1320b5dc13aa91dbac6eabc346cb655592aef8244a8ed04b8c4b3bdd59b8af4c","type":"regular"},'
                '{"mode":"100755","path":"脚本.sh","sha256":"306c6ca7407560340797866e077e053627ad409277d1b9da58106fce4cf717cb","type":"regular"}],"schema_version":1}').encode('utf-8')
VECTOR_DIGEST = '80a31269779dfebdcd6c7c837b00f973b9bcb099257defc007ccfb752cfc3068'


class SingleTaskScopeTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        self.tasks = self.root/TASKS
        (self.root/'outside.md').write_bytes(b'Unrelated baseline\n')
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: unrelated tracked baseline')
        (self.root/'.git/info').mkdir(exist_ok=True)
        with (self.root/'.git/info/exclude').open('a') as stream:
            stream.write('\n.symphony/\n')
        self.context = self.fixture.context()
        self.context['checks'] = [{'id': 'must-not-run', 'argv': [sys.executable, '-c',
                                  'from pathlib import Path; Path("acceptance-ran").touch()'],
                                  'cwd': '.', 'timeout_seconds': 10, 'expected_exit': 0}]
        sys.path.insert(0, str(ROOT/'scripts'))
        self.addCleanup(sys.path.remove, str(ROOT/'scripts'))
        self.scope = self.module('single_task_scope')
        self.state = self.module('single_task_state')

    def module(self, name):
        spec = importlib.util.spec_from_file_location(name, ROOT/'scripts'/f'{name}.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def begin(self, allowed=None):
        if allowed is not None:
            self.context['allowed_paths'] = allowed
        self.context['approval']['context_digest'] = context_digest(self.context)
        path = self.fixture.private/'context.json'
        path.write_bytes(canonical_bytes(self.context))
        result = self.cli('begin', '--context', str(path))
        self.assertEqual(0, result.returncode, 'Validated T010 fixture entry must succeed')
        response = json.loads(result.stdout)
        self.assertEqual('active', response['progress'])
        self.assertFalse(response['owner_identity_authenticated'])
        self.assertFalse(response['live_dispatch_verified'])
        self.assertEqual([], response['errors'])
        self.attempt_id = response['attempt_id']
        with self.state.open_store(self.root, require_existing=True) as store:
            self.attempt = store.read_attempt(self.attempt_id)
        self.assertEqual(self.context, self.attempt['context'])
        self.assertEqual(self.tasks.read_bytes(), self.attempt['tasks_bytes'])
        status = self.cli('status', '--attempt', self.attempt_id)
        self.assertEqual(0, status.returncode, 'Validated T010 fixture status must succeed')
        self.assertEqual(self.attempt_id, json.loads(status.stdout)['attempt_id'])
        self.assertFalse((self.root/'acceptance-ran').exists())

    def cli(self, *args):
        return subprocess.run([sys.executable, '-B', str(ROOT/'scripts/single_task.py'), *args],
                              cwd=self.root, env=self.fixture.env, capture_output=True,
                              text=True, timeout=15)

    def owning(self, name, successor):
        function = getattr(self.scope, name, None)
        self.assertTrue(callable(function), f'{successor} owning behavior missing: {name}')
        return function

    def manifest(self):
        return self.fixture.manifest(self.context['allowed_paths'] + [TASKS], 'T001')

    def inventory(self):
        return self.owning('validate_scope', 'T013')(self.root, self.attempt)

    def rejected_inventory(self):
        function = self.owning('validate_scope', 'T013')
        before = self.tasks.read_bytes()
        with self.assertRaises(ValueError):
            function(self.root, self.attempt)
        self.assertEqual(before, self.tasks.read_bytes())
        self.assertFalse((self.root/'acceptance-ran').exists())

    def commit(self):
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: scope scenario')

    def restore_commit(self, path, original):
        path = self.root/path
        if path.is_symlink() or path.is_file():
            path.unlink()
        if original is not None:
            path.write_bytes(original)
        self.commit()

    def git_plumbing(self, *args):
        # Only fixed local object/index plumbing; never transports or hooks.
        self.assertIn(args[0], ('update-index',))
        return subprocess.run(['git', '-c', f'core.hooksPath={self.fixture.hooks}',
                               '-c', 'core.fsmonitor=false', *args], cwd=self.root,
                              env=self.fixture.env, capture_output=True, check=True, timeout=10)

    def review(self, manifest=None):
        value = self.fixture.review(self.context)
        value['attempt_id'] = self.attempt_id
        value['content_manifest_digest'] = digest(canonical_bytes(manifest or self.manifest()))
        return value

    def rejected_review(self, value, manifest=None):
        function = self.owning('validate_semantic_review', 'T014')
        before = self.tasks.read_bytes()
        with self.assertRaises(ValueError):
            function(value, self.attempt, manifest or self.manifest())
        self.assertEqual(before, self.tasks.read_bytes())
        self.assertFalse((self.root/'acceptance-ran').exists())

    def test_allowed_worktree_edits(self):
        self.begin()
        (self.root/'guide.md').write_bytes(b'Allowed current\n')
        self.assertEqual(self.manifest(), self.inventory())

    def test_allowed_staged_edits(self):
        self.begin()
        (self.root/'guide.md').write_bytes(b'Allowed index\n')
        self.fixture.git('add', 'guide.md')
        self.assertEqual(self.manifest(), self.inventory())

    def test_allowed_committed_edits(self):
        self.begin()
        (self.root/'guide.md').write_bytes(b'Allowed commit\n')
        self.commit()
        self.assertEqual(self.manifest(), self.inventory())

    def test_reject_unauthorized_worktree_change(self):
        self.begin()
        (self.root/'outside.md').write_bytes(b'Unauthorized\n')
        self.rejected_inventory()

    def test_reject_staged_change_hidden_by_worktree_restore(self):
        self.begin()
        path = self.root/'outside.md'
        original = path.read_bytes()
        path.write_bytes(b'Unauthorized index\n')
        self.fixture.git('add', 'outside.md')
        path.write_bytes(original)
        self.rejected_inventory()

    def test_reject_nonignored_untracked_change(self):
        self.begin()
        (self.root/'unexpected.txt').write_bytes(b'Unauthorized untracked\n')
        self.rejected_inventory()

    def test_reject_committed_change_hidden_by_revert(self):
        self.begin()
        path = self.root/'outside.md'
        original = path.read_bytes()
        path.write_bytes(b'Unauthorized commit\n')
        self.commit()
        self.restore_commit('outside.md', original)
        self.assertEqual(original, path.read_bytes())
        self.rejected_inventory()

    def test_physical_executable_mode_in_manifest(self):
        self.fixture.git('config', 'core.filemode', 'false')
        self.begin()
        (self.root/'guide.md').chmod(0o755)
        self.assertEqual(self.manifest(), self.inventory())

    def test_reject_directory_output(self):
        self.begin()
        path = self.root/'guide.md'
        path.unlink()
        path.mkdir()
        self.rejected_inventory()

    def test_reject_symlink_worktree(self):
        self.begin()
        target = self.fixture.private/'target'
        target.write_bytes(b'Outside target\n')
        path = self.root/'guide.md'
        path.unlink()
        path.symlink_to(target)
        self.rejected_inventory()
        self.assertEqual(b'Outside target\n', target.read_bytes())

    def test_reject_staged_symlink_hidden_by_worktree_restore(self):
        self.begin()
        path = self.root/'guide.md'
        original = path.read_bytes()
        path.unlink()
        path.symlink_to('outside.md')
        self.fixture.git('add', 'guide.md')
        path.unlink()
        path.write_bytes(original)
        self.rejected_inventory()

    def test_reject_historical_symlink_hidden_by_revert(self):
        self.begin()
        path = self.root/'guide.md'
        original = path.read_bytes()
        path.unlink()
        path.symlink_to('outside.md')
        self.commit()
        self.restore_commit('guide.md', original)
        self.rejected_inventory()

    def test_reject_index_gitlink(self):
        self.begin(['guide.md', 'module'])
        self.git_plumbing('update-index', '--add', '--cacheinfo',
                          '160000,' + self.attempt['baseline'] + ',module')
        self.rejected_inventory()

    def test_reject_historical_gitlink_hidden_by_revert(self):
        self.begin(['guide.md', 'module'])
        self.git_plumbing('update-index', '--add', '--cacheinfo',
                          '160000,' + self.attempt['baseline'] + ',module')
        self.fixture.git('commit', '-m', 'test: local gitlink')
        self.git_plumbing('update-index', '--force-remove', 'module')
        self.fixture.git('commit', '-m', 'test: remove local gitlink')
        self.rejected_inventory()

    def test_reject_other_task_marker(self):
        self.begin()
        self.tasks.write_bytes(self.tasks.read_bytes().replace(b'[ ] T002', b'[x] T002'))
        self.rejected_inventory()

    def test_reject_selected_task_text(self):
        self.begin()
        self.tasks.write_bytes(self.tasks.read_bytes().replace(b'Write the synthetic guide', b'Changed task description'))
        self.rejected_inventory()

    def test_reject_task_order(self):
        self.begin()
        lines = self.tasks.read_bytes().splitlines(keepends=True)
        rows = [index for index, line in enumerate(lines) if line.startswith(b'- [ ] T')]
        lines[rows[0]], lines[rows[1]] = lines[rows[1]], lines[rows[0]]
        self.tasks.write_bytes(b''.join(lines))
        self.rejected_inventory()

    def test_reject_staged_task_mutation_hidden_by_worktree_restore(self):
        self.begin()
        original = self.tasks.read_bytes()
        self.tasks.write_bytes(original.replace(b'[ ] T002', b'[x] T002'))
        self.fixture.git('add', TASKS)
        self.tasks.write_bytes(original)
        self.rejected_inventory()

    def test_reject_historical_task_mutation_hidden_by_revert(self):
        self.begin()
        original = self.tasks.read_bytes()
        self.tasks.write_bytes(original.replace(b'[ ] T002', b'[x] T002'))
        self.commit()
        self.restore_commit(TASKS, original)
        self.rejected_inventory()

    def test_absent_is_distinct_from_sentinel_file(self):
        self.begin(['guide.md', 'missing.txt', 'sentinel.txt'])
        (self.root/'sentinel.txt').write_bytes(b'absent')
        manifest = self.inventory()
        absent = next(item for item in manifest['entries'] if item['path'] == 'missing.txt')
        regular = next(item for item in manifest['entries'] if item['path'] == 'sentinel.txt')
        self.assertEqual({'path': 'missing.txt', 'type': 'absent', 'mode': None, 'sha256': None}, absent)
        self.assertEqual('regular', regular['type'])
        self.assertEqual(digest(b'absent'), regular['sha256'])
        self.assertNotEqual(absent['type'], regular['type'])

    def test_canonical_unicode_binary_mode_and_absence_vector(self):
        self.begin()
        manifest = json.loads(VECTOR_BYTES)
        function = self.owning('canonical_manifest_bytes', 'T014')
        actual = function(manifest)
        self.assertEqual(VECTOR_BYTES, actual)
        self.assertEqual(VECTOR_DIGEST, digest(actual))

    def test_fixed_checkbox_normalization_vector(self):
        self.begin()
        function = self.owning('normalize_tasks_bytes', 'T014')
        checked = b'- [x] T001 Scope\r\n- [ ] T002 Later\r\n'
        self.assertEqual(VECTOR_TASKS, function(VECTOR_TASKS, 'T001'))
        normalized = function(checked, 'T001')
        self.assertEqual(VECTOR_TASKS, normalized)
        self.assertEqual('fdc2587b398411f49f470266e66d59235464a2fbfa6ab66860b6545e5b4f2d5b',
                         digest(normalized))

    def test_fixed_normalization_preserves_other_checked_marker(self):
        self.begin()
        function = self.owning('normalize_tasks_bytes', 'T014')
        checked = b'- [x] T001 Scope\r\n- [x] T002 Later\r\n'
        expected = b'- [ ] T001 Scope\r\n- [x] T002 Later\r\n'
        normalized = function(checked, 'T001')
        self.assertEqual(expected, normalized)
        self.assertEqual('b2e9d39e0ce8cb0105968ed26925edeef2ee3ac1cfff6e3527f4710b22da744c',
                         digest(normalized))

    def test_normalize_only_selected_checkbox_with_crlf(self):
        # Retain the dependency table while pinning raw CRLF task bytes.
        original = self.tasks.read_bytes()
        self.tasks.write_bytes(original.replace(b'\n', b'\r\n'))
        self.commit()
        commit = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        for name in ('spec.md', 'plan.md', 'tasks.md'):
            self.context['reviewed_artifacts'][name].update(commit=commit,
                sha256=digest((self.fixture.feature/name).read_bytes()))
        self.context['source_assertion']['sha256'] = digest(self.tasks.read_bytes())
        self.begin()
        raw_before = self.tasks.read_bytes()
        offset = raw_before.index(b'[ ] T001') + 1
        raw_after = raw_before[:offset] + b'x' + raw_before[offset+1:]
        self.tasks.write_bytes(raw_after)
        after = self.inventory()
        self.assertEqual(self.manifest(), after)
        self.assertEqual(raw_before, self.attempt['tasks_bytes'])
        self.assertEqual(raw_after, self.tasks.read_bytes())
        task_entry = next(item for item in after['entries'] if item['path'] == TASKS)
        self.assertEqual(digest(raw_before), task_entry['sha256'])

    def test_valid_manifest_schema(self):
        self.begin(['guide.md', 'missing.txt'])
        value = self.manifest()
        function = self.owning('validate_manifest', 'T013')
        self.assertEqual(value, function(value))

    def test_valid_semantic_review(self):
        self.begin()
        value = self.review()
        function = self.owning('validate_semantic_review', 'T014')
        self.assertEqual(value, function(value, self.attempt, self.manifest()))

    def test_reject_missing_semantic_review(self):
        self.begin()
        self.rejected_review(None)

    def test_reject_negative_semantic_review(self):
        self.begin()
        value = self.review()
        value['approved'] = False
        self.rejected_review(value)

    def test_reject_review_stale_after_output_bytes_change(self):
        self.begin()
        value = self.review()
        (self.root/'guide.md').write_bytes(b'Changed after inspection\n')
        self.rejected_review(value)

    def test_reject_review_stale_after_output_mode_change(self):
        self.fixture.git('config', 'core.filemode', 'false')
        self.begin()
        value = self.review()
        (self.root/'guide.md').chmod(0o755)
        self.rejected_review(value)

    def test_reject_review_stale_after_absent_output_creation(self):
        self.begin(['guide.md', 'missing.txt'])
        value = self.review()
        (self.root/'missing.txt').write_bytes(b'Now exists\n')
        self.rejected_review(value)

    def test_inspection_without_checks_task_or_acceptance_mutation(self):
        self.begin(['guide.md', 'missing.txt'])
        function = self.owning('prepare_inspection', 'T014')
        before = self.tasks.read_bytes()
        journal = self.root/'.symphony/single-task/journal.sqlite3'
        with sqlite3.connect(journal) as connection:
            events_before = connection.execute('SELECT * FROM events').fetchall()
        result = function(self.root, self.attempt)
        expected = self.manifest()
        self.assertEqual({'attempt_id': self.attempt_id, 'task_id': 'T001',
                          'context_digest': self.context['approval']['context_digest'],
                          'content_manifest': expected,
                          'content_manifest_digest': digest(canonical_bytes(expected))}, result)
        self.assertFalse((self.root/'acceptance-ran').exists())
        self.assertEqual(before, self.tasks.read_bytes())
        with sqlite3.connect(journal) as connection:
            self.assertEqual(events_before, connection.execute('SELECT * FROM events').fetchall())


def manifest_rejection(mutation):
    def test(self):
        self.begin(['guide.md', 'missing.txt'])
        value = self.manifest()
        mutation(value)
        function = self.owning('validate_manifest', 'T013')
        with self.assertRaises(ValueError):
            function(value)
    return test


def review_rejection(mutation):
    def test(self):
        self.begin()
        value = self.review()
        mutation(value)
        self.rejected_review(value)
    return test


# Generate independent collected IDs, with one body assertion per schema boundary.
for field in ('schema_version', 'entries'):
    setattr(SingleTaskScopeTests, 'test_manifest_missing_' + field,
            manifest_rejection(lambda value, field=field: value.pop(field)))
for field in ('path', 'type', 'mode', 'sha256'):
    setattr(SingleTaskScopeTests, 'test_manifest_entry_missing_' + field,
            manifest_rejection(lambda value, field=field: value['entries'][0].pop(field)))
for name, mutate in {
        'unknown_field': lambda value: value.update(extra=True),
        'entry_unknown_field': lambda value: value['entries'][0].update(extra=True),
        'boolean_version': lambda value: value.update(schema_version=True),
        'unsupported_version': lambda value: value.update(schema_version=2),
        'entries_wrong_type': lambda value: value.update(entries={}),
        'unsafe_path': lambda value: value['entries'][0].update(path='../outside'),
        'wrong_type': lambda value: value['entries'][0].update(type='symlink'),
        'wrong_mode': lambda value: value['entries'][0].update(mode='120000'),
        'wrong_digest': lambda value: value['entries'][0].update(sha256='A' * 64),
        'absent_nonnull_mode': lambda value: value['entries'][1].update(mode='100644'),
        'absent_nonnull_digest': lambda value: value['entries'][1].update(sha256='0' * 64),
        'duplicate_path': lambda value: value['entries'].append(copy.deepcopy(value['entries'][0])),
        'unsorted_paths': lambda value: value['entries'].reverse(),
}.items():
    setattr(SingleTaskScopeTests, 'test_manifest_reject_' + name, manifest_rejection(mutate))

for field in ('schema_version', 'attempt_id', 'task_id', 'context_digest',
              'content_manifest_digest', 'reviewer_ref', 'evidence_ref', 'approved'):
    setattr(SingleTaskScopeTests, 'test_review_missing_' + field,
            review_rejection(lambda value, field=field: value.pop(field)))
    setattr(SingleTaskScopeTests, 'test_review_wrong_type_' + field,
            review_rejection(lambda value, field=field: value.update({field: []})))
for field in ('attempt_id', 'task_id', 'context_digest', 'content_manifest_digest'):
    different = 'T002' if field == 'task_id' else '0' * 64
    setattr(SingleTaskScopeTests, 'test_review_mismatched_' + field,
            review_rejection(lambda value, field=field, different=different: value.update({field: different})))
for name, mutate in {
        'unknown_field': lambda value: value.update(extra=True),
        'boolean_version': lambda value: value.update(schema_version=True),
        'unsupported_version': lambda value: value.update(schema_version=2),
        'empty_reviewer_ref': lambda value: value.update(reviewer_ref=''),
        'empty_evidence_ref': lambda value: value.update(evidence_ref=''),
}.items():
    setattr(SingleTaskScopeTests, 'test_review_reject_' + name, review_rejection(mutate))
