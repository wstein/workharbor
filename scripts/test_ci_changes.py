"""Docs-only pull request classification must fail closed."""
import importlib.util
import io
import os
import subprocess
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

SPEC = importlib.util.spec_from_file_location('ci_changes', Path(__file__).with_name('ci_changes.py'))
changes = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(changes)

REGULAR = '100644'


def entry(path, old=REGULAR, new=REGULAR):
    return (old, new, path)


class ClassifyTests(unittest.TestCase):
    def test_prose_outside_pinned_paths_is_docs_only(self):
        for path in ('docs/adr/0009-new.md', 'docs/exec-plans/x.md', 'docs/product.md', 'docs/design/a/b.md',
                     'docs/archive/workharbor-old/docs/content/x.md', 'specs/006-whr-cli/spec.md',
                     'specs/009-host-setup-wizard/contracts/protocol.md'):
            with self.subTest(path=path):
                self.assertTrue(changes.docs_only([entry(path)]))

    def test_added_and_deleted_prose_is_docs_only(self):
        self.assertTrue(changes.docs_only([entry('docs/adr/a.md', '000000'), entry('docs/adr/b.md', new='000000')]))

    def test_paths_read_or_hash_pinned_by_checks_are_not_docs(self):
        # Each is read by a test or pinned by hash/tree id; a prose edit can turn it red.
        for path in ('README.md', 'AGENTS.md', 'WORKFLOW.md', 'docs/releasing.md', 'docs/container-runtime.md',
                     'docs/development/symphony.md', 'docs/development/devcontainer-matrix.md',
                     'specs/001-workspace-lifecycle/tasks.md', 'specs/002-symphony-doc-smoke/spec.md',
                     'specs/003-single-task-entry/plan.md', 'specs/005-provider-aware-orchestration/contracts/cli-and-host.md',
                     '.specify/memory/constitution.md', 'contracts/README.md', 'tests/fixtures/single-task/spec.md',
                     'apps/whr/README.md'):
            with self.subTest(path=path):
                self.assertFalse(changes.docs_only([entry(path)]))

    def test_anything_else_is_not_docs(self):
        for path in ('scripts/check_ci.py', '.github/workflows/check.yml', 'apps/whr/main.go', 'docs/adr/a.json',
                     'docs/adr/a.md.sh', 'docs/adr/a.MD', 'docs/adr', 'docs/adrx/a.md', 'docs/product.md/x', '.tool-versions',
                     'Makefile', 'contracts/cli/commands.json', 'docs/adr/../../scripts/x.md', '/docs/adr/a.md',
                     'docs/adr/a\n.md', 'docs/adr/a.md\\x', ''):
            with self.subTest(path=path):
                self.assertFalse(changes.docs_only([entry(path)]))

    def test_one_unknown_path_makes_the_whole_change_full(self):
        self.assertFalse(changes.docs_only([entry('docs/adr/a.md'), entry('scripts/x.py')]))

    def test_empty_change_is_unsure(self):
        self.assertFalse(changes.docs_only([]))

    def test_symlink_executable_and_submodule_modes_are_not_docs(self):
        for mode in ('120000', '100755', '160000', '040000', 'zzz'):
            for old, new in ((mode, REGULAR), (REGULAR, mode), (mode, mode)):
                with self.subTest(old=old, new=new):
                    self.assertFalse(changes.docs_only([entry('docs/adr/a.md', old, new)]))


class GitTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.env = {key: value for key, value in os.environ.items() if not key.startswith('GIT_')}
        self.env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_SYSTEM=os.devnull, GIT_AUTHOR_NAME='t',
                        GIT_AUTHOR_EMAIL='t@example.invalid', GIT_COMMITTER_NAME='t', GIT_COMMITTER_EMAIL='t@example.invalid')
        self.git('init', '-q', '-b', 'main')
        self.write('docs/adr/a.md', 'a\n')
        self.write('docs/adr/old.md', 'old\n')
        self.write('scripts/x.py', 'x = 1\n')
        self.base = self.commit('base')

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.root, env=self.env, check=True,
                              capture_output=True, text=True).stdout.strip()

    def write(self, name, text):
        path = self.root/name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def commit(self, message):
        self.git('add', '-A')
        self.git('commit', '-q', '-m', message)
        return self.git('rev-parse', 'HEAD')

    def classify(self, base=None, head=None, event='pull_request'):
        return changes.classify(self.root, event, base or self.base, head or self.git('rev-parse', 'HEAD'), env=self.env)

    def test_prose_edit_add_and_delete(self):
        self.write('docs/adr/a.md', 'changed\n')
        self.write('docs/adr/new.md', 'new\n')
        (self.root/'docs/adr/old.md').unlink()
        self.commit('docs')
        self.assertTrue(self.classify())

    def test_rename_out_of_prose_counts_the_source_and_target(self):
        self.git('mv', 'scripts/x.py', 'docs/adr/x.md')
        self.commit('move')
        self.assertFalse(self.classify())

    def test_code_change_is_full(self):
        self.write('docs/adr/a.md', 'changed\n')
        self.write('scripts/x.py', 'x = 2\n')
        self.commit('mixed')
        self.assertFalse(self.classify())

    def test_symlink_and_mode_changes_are_full(self):
        (self.root/'docs/adr/a.md').unlink()
        (self.root/'docs/adr/a.md').symlink_to('../../scripts/x.py')
        self.commit('link')
        self.assertFalse(self.classify())
        (self.root/'docs/adr/a.md').unlink()
        self.write('docs/adr/a.md', 'a\n')
        (self.root/'docs/adr/a.md').chmod(0o755)
        self.commit('mode')
        self.assertFalse(self.classify(base=self.git('rev-parse', 'HEAD~1')))

    def test_diff_is_against_the_merge_base_not_the_moved_base(self):
        self.git('checkout', '-q', '-b', 'topic')
        self.write('docs/adr/a.md', 'topic\n')
        head = self.commit('topic')
        self.git('checkout', '-q', 'main')
        self.write('scripts/x.py', 'x = 3\n')
        moved = self.commit('main moved')
        self.assertTrue(self.classify(base=moved, head=head))

    def test_unsure_inputs_are_full(self):
        self.write('docs/adr/a.md', 'changed\n')
        head = self.commit('docs')
        self.assertTrue(self.classify(head=head))
        self.assertFalse(self.classify(event='push'))
        self.assertFalse(self.classify(event=''))
        self.assertFalse(self.classify(base='0' * 40, head=head))
        self.assertFalse(self.classify(base='main', head=head))
        self.assertFalse(self.classify(base=self.base.upper(), head=head))
        self.assertFalse(self.classify(base=self.base, head=self.base))
        self.assertFalse(changes.classify(self.root/'missing', 'pull_request', self.base, head, env=self.env))

    def test_main_writes_output_and_never_raises(self):
        self.write('docs/adr/a.md', 'changed\n')
        head = self.commit('docs')
        output = self.root.parent/(self.root.name + '.out')
        self.addCleanup(lambda: output.exists() and output.unlink())
        env = dict(self.env, GITHUB_OUTPUT=str(output))
        args = ['--root', str(self.root), '--event', 'pull_request', '--base', self.base, '--head', head]
        with redirect_stdout(io.StringIO()):
            self.assertEqual(0, changes.main(args, env=env))
            self.assertEqual(0, changes.main(args[:-1] + ['not-a-sha'], env=env))
            with redirect_stderr(io.StringIO()):
                self.assertEqual(0, changes.main(['--bogus'], env=env))
        self.assertEqual('docs_only=true\ndocs_only=false\ndocs_only=false\n', output.read_text())


if __name__ == '__main__':
    unittest.main()
