"""T002 local data boundaries; no package/guard/execution authority."""
import math
import os
from pathlib import Path
import subprocess
import stat
import tempfile
import traceback
import unittest
from unittest.mock import patch

import task_package as package


class JsonPrimitiveTests(unittest.TestCase):
    def reject(self, data, code='invalid-json'):
        with self.assertRaises(package.PackageError) as raised:
            package.parse_json(data)
        self.assertEqual(code, raised.exception.code)
        self.assertNotIn('secret-canary', str(raised.exception))

    def test_strict_utf8_and_unicode(self):
        self.assertEqual({'name': 'Grüße'}, package.parse_json('{"name":"Grüße"}'.encode()))
        for value in (b'"\xff"', b'\xef\xbb\xbf{}', b'"\\ud800"', b'"\\udfff"'):
            with self.subTest(value=value):
                self.reject(value)

    def test_duplicate_keys_at_every_depth(self):
        for value in (b'{"secret-canary":1,"secret-canary":2}',
                      b'{"nested":[{"a":1,"a":2}]}'):
            self.reject(value)

    def test_nonfinite_including_overflow(self):
        for value in (b'NaN', b'Infinity', b'-Infinity', b'1e9999', b'{"a":[1e999]}'):
            self.reject(value)

    def test_malformed_trailing_and_nonbytes(self):
        for value in (b'', b'{secret-canary}', b'{} true', '{}', None):
            self.reject(value)

    def test_byte_limit_is_inclusive_and_never_truncates(self):
        self.assertEqual('x' * (package.MAX_JSON_BYTES - 2),
                         package.parse_json(b'"' + b'x' * (package.MAX_JSON_BYTES - 2) + b'"'))
        self.reject(b' ' * package.MAX_JSON_BYTES + b'{}', 'input-limit-exceeded')

    def test_deep_input_has_safe_failure(self):
        self.reject(b'[' * 2000 + b'0' + b']' * 2000)

    def test_exact_required_optional_keys(self):
        value = {'required': 1, 'optional': 2}
        self.assertIs(value, package.require_keys(value, {'required'}, {'optional'}))
        for value in ({'secret-canary': 1}, {}, [], {'required': 1, 'secret-canary': 2}):
            with self.subTest(value=value), self.assertRaises(package.PackageError) as raised:
                package.require_keys(value, {'required'}, {'optional'})
            self.assertEqual('invalid-object-keys', raised.exception.code)
            self.assertNotIn('secret-canary', str(raised.exception))

    def test_nested_keys_are_checked_explicitly_without_new_context_schema(self):
        value = package.parse_json(b'{"opaque":{"secret-canary":true}}')
        package.require_keys(value, {'opaque'})
        with self.assertRaises(package.PackageError):
            package.require_keys(value['opaque'], {'required'})

    def test_canonical_bytes(self):
        self.assertEqual(b'{"a":"Gr\xc3\xbc\xc3\x9fe","z":[1,true,null]}',
                         package.canonical_json({'z': [1, True, None], 'a': 'Grüße'}))

    def test_canonical_rejects_non_json_values(self):
        for value in (math.nan, math.inf, {'a': math.inf}, {1: 'key'}, ('tuple',),
                      {'a': '\ud800'}, object()):
            with self.subTest(kind=type(value)), self.assertRaises(package.PackageError):
                package.canonical_json(value)


class RepositoryPrimitiveTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve() / 'repository'
        self.root.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.name', 'Offline Fixture')
        self.git('config', 'user.email', 'fixture@example.invalid')
        (self.root / 'data').mkdir()
        (self.root / 'data/input.json').write_bytes(b'{"name":"safe"}')
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')
        self.commit = self.git('rev-parse', 'HEAD').decode().strip()

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.root, check=True,
                              capture_output=True).stdout

    def test_regular_contained_read_and_json(self):
        self.assertEqual(b'{"name":"safe"}',
                         package.read_repository_file(self.root, 'data/input.json'))
        self.assertEqual({'name': 'safe'},
                         package.load_json(self.root, 'data/input.json', required={'name'}))
        with self.assertRaises(package.PackageError):
            package.load_json(self.root, 'data/input.json', required={'missing'})
        self.assertFalse((self.root / '.symphony').exists())

    def test_noncanonical_paths(self):
        for value in ('', '.', '..', '/data/input.json', './data/input.json',
                      'data//input.json', 'data/../input.json', 'data/input.json/',
                      'data\\input.json', 'data/\x00secret-canary', 'data/\nsecret-canary',
                      '.git/config', '.symphony/state', 'data/\ud800', None):
            with self.subTest(value=value), self.assertRaises(package.PackageError) as raised:
                package.read_repository_file(self.root, value)
            self.assertNotIn('secret-canary', str(raised.exception))

    def test_missing_directory_and_file(self):
        for value in ('absent/input.json', 'data/absent.json'):
            with self.subTest(value=value), self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, value)

    def test_symlink_files_and_parents_even_when_contained(self):
        (self.root / 'link.json').symlink_to('data/input.json')
        (self.root / 'parent').symlink_to('data', target_is_directory=True)
        for value in ('link.json', 'parent/input.json'):
            with self.subTest(value=value), self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, value)

    def test_root_and_ancestor_symlinks(self):
        link = self.root.parent / 'alias'
        link.symlink_to(self.root, target_is_directory=True)
        nested = self.root / 'data'
        for root in (link, link / 'data', Path('relative-root')):
            with self.subTest(root=root), self.assertRaises(package.PackageError):
                package.read_repository_file(root, 'input.json')
        self.assertTrue(nested.is_dir())

    def test_hardlink_directory_fifo_and_socket_rejected(self):
        os.link(self.root / 'data/input.json', self.root / 'hard.json')
        os.mkfifo(self.root / 'pipe')
        for value in ('hard.json', 'data/input.json', 'data', 'pipe'):
            with self.subTest(value=value), self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, value)
        # Native socket binding is denied in the host sandbox. Inject the same
        # nonregular stat mode at the type-check boundary without opening one.
        (self.root / 'socket').write_bytes(b'{}')
        original = package.os.stat
        def socket_metadata(name, *args, **kwargs):
            info = original(name, *args, **kwargs)
            if name == 'socket':
                values = list(info)
                values[0] = stat.S_IFSOCK | 0o600
                return os.stat_result(values)
            return info
        with patch.object(package.os, 'stat', side_effect=socket_metadata):
            with self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, 'socket')

    def test_oversized_file(self):
        (self.root / 'large').write_bytes(b'x' * (package.MAX_JSON_BYTES + 1))
        with self.assertRaises(package.PackageError) as raised:
            package.read_repository_file(self.root, 'large')
        self.assertEqual('input-limit-exceeded', raised.exception.code)

    def test_gitlink_and_descendants_rejected(self):
        self.git('update-index', '--add', '--cacheinfo', '160000,' + self.commit + ',submodule')
        (self.root / 'submodule').mkdir()
        (self.root / 'submodule/input.json').write_bytes(b'{}')
        with self.assertRaises(package.PackageError) as raised:
            package.read_repository_file(self.root, 'submodule/input.json')
        self.assertEqual('unsafe-repository-path', raised.exception.code)

    def test_literal_git_pathspec(self):
        (self.root / ':(glob)*.json').write_bytes(b'{}')
        self.assertEqual(b'{}', package.read_repository_file(self.root, ':(glob)*.json'))

    def test_file_replaced_between_stat_and_open(self):
        original = package.os.open
        def replaced(name, flags, *args, **kwargs):
            if name == 'input.json':
                old = self.root / 'data/input.json'
                old.rename(self.root / 'old.json')
                old.write_bytes(b'{"name":"race"}')
            return original(name, flags, *args, **kwargs)
        with patch.object(package.os, 'open', side_effect=replaced), self.assertRaises(package.PackageError):
            package.read_repository_file(self.root, 'data/input.json')

    def test_file_changed_during_read(self):
        original = package.os.read
        changed = False
        inode = (self.root / 'data/input.json').stat().st_ino
        def modified(fd, length):
            nonlocal changed
            data = original(fd, length)
            if not changed and os.fstat(fd).st_ino == inode:
                changed = True
                (self.root / 'data/input.json').write_bytes(b'{"name":"race"}')
            return data
        with patch.object(package.os, 'read', side_effect=modified), self.assertRaises(package.PackageError):
            package.read_repository_file(self.root, 'data/input.json')

    def test_parent_replaced_during_read(self):
        original = package.os.read
        changed = False
        inode = (self.root / 'data/input.json').stat().st_ino
        def replaced(fd, length):
            nonlocal changed
            data = original(fd, length)
            if not changed and os.fstat(fd).st_ino == inode:
                changed = True
                (self.root / 'data').rename(self.root / 'old-data')
                (self.root / 'data').mkdir()
                (self.root / 'data/input.json').write_bytes(data)
            return data
        with patch.object(package.os, 'read', side_effect=replaced), self.assertRaises(package.PackageError):
            package.read_repository_file(self.root, 'data/input.json')

    def test_file_changed_during_final_index_probe(self):
        original = package._git_index_entry
        probes = 0
        def modified(root, path):
            nonlocal probes
            entries = original(root, path)
            probes += 1
            if probes == 4:
                (self.root / 'data/input.json').write_bytes(b'{"name":"race"}')
            return entries
        with patch.object(package, '_git_index_entry', side_effect=modified):
            with self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, 'data/input.json')

    def test_staged_symlink_mode_is_rejected_even_with_regular_worktree_file(self):
        blob = self.git('rev-parse', 'HEAD:data/input.json').decode().strip()
        self.git('update-index', '--cacheinfo', '120000,' + blob + ',data/input.json')
        with self.assertRaises(package.PackageError):
            package.read_repository_file(self.root, 'data/input.json')

    def test_raw_os_errors_do_not_escape_diagnostics(self):
        with patch.object(package.os, 'read', side_effect=OSError('secret-canary')):
            try:
                package.read_repository_file(self.root, 'data/input.json')
            except package.PackageError as error:
                self.assertNotIn('secret-canary', ''.join(traceback.format_exception(error)))
            else:
                self.fail('read should reject')

    def test_exact_local_commit_tree_and_blob(self):
        tree = self.git('rev-parse', 'HEAD^{tree}').decode().strip()
        blob = self.git('rev-parse', 'HEAD:data/input.json').decode().strip()
        for identity, kind in ((self.commit, 'commit'), (tree, 'tree'), (blob, 'blob')):
            self.assertEqual(identity, package.resolve_git_object(self.root, identity, expected_type=kind))
        with self.assertRaises(package.PackageError):
            package.resolve_git_object(self.root, blob)

    def test_nonfull_refs_missing_ids_and_revision_expressions(self):
        for value in ('HEAD', self.commit[:12], self.commit.upper(), self.commit + '^{commit}',
                      '0' * 40, '--secret-canary', None):
            with self.subTest(value=value), self.assertRaises(package.PackageError) as raised:
                package.resolve_git_object(self.root, value)
            self.assertNotIn('secret-canary', str(raised.exception))

    def test_inherited_git_environment_cannot_redefine_lookup(self):
        with patch.dict(os.environ, {'GIT_DIR': '/secret-canary/absent',
                                     'GIT_CONFIG_COUNT': '1', 'GIT_CONFIG_KEY_0': 'core.bare',
                                     'GIT_CONFIG_VALUE_0': 'true'}):
            self.assertEqual(self.commit, package.resolve_git_object(self.root, self.commit))

    def test_replacement_refs_and_grafts_rejected(self):
        self.git('replace', self.commit, self.commit)
        with self.assertRaises(package.PackageError):
            package.resolve_git_object(self.root, self.commit)
        self.git('replace', '-d', self.commit)
        (self.root / '.git/info/grafts').write_text(self.commit + '\n')
        with self.assertRaises(package.PackageError):
            package.resolve_git_object(self.root, self.commit)

    def test_git_lookup_disables_lazy_fetch_and_preserves_no_raw_errors(self):
        original = package.subprocess.run
        observed = []
        def local_only(argv, **kwargs):
            self.assertIn('--no-lazy-fetch', argv)
            self.assertEqual('1', kwargs['env']['GIT_NO_LAZY_FETCH'])
            self.assertEqual('', kwargs['env']['GIT_ALLOW_PROTOCOL'])
            self.assertNotIn('fetch', argv)
            observed.append(argv)
            return original(argv, **kwargs)
        with patch.object(package.subprocess, 'run', side_effect=local_only):
            self.assertEqual(self.commit, package.resolve_git_object(self.root, self.commit))
            with self.assertRaises(package.PackageError):
                package.resolve_git_object(self.root, '0' * 40)
        self.assertTrue(observed)
        with patch.object(package.subprocess, 'run', side_effect=OSError('secret-canary')):
            with self.assertRaises(package.PackageError) as raised:
                package.resolve_git_object(self.root, self.commit)
        self.assertNotIn('secret-canary', str(raised.exception))


if __name__ == '__main__':
    unittest.main()
