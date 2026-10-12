import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

class SymphonyTests(unittest.TestCase):
    def test_prepare_fails_without_environment(self):
        env = {k:v for k,v in os.environ.items() if not k.startswith('WORKHARBOR_') and k != 'GITHUB_TOKEN'}
        run = subprocess.run(['python3', str(ROOT/'scripts/prepare_symphony.py')], env=env, capture_output=True, text=True)
        self.assertNotEqual(0, run.returncode)
        self.assertIn('Missing settings', run.stderr)

    def test_render_preserves_secret_reference(self):
        # Isolated fake CLI checks the render contract only, not actual authentication.
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            project = base/'source'
            (project/'scripts').mkdir(parents=True)
            (project/'WORKFLOW.md').write_text((ROOT/'WORKFLOW.md').read_text())
            script = project/'scripts/prepare_symphony.py'
            script.write_text((ROOT/'scripts/prepare_symphony.py').read_text())
            fake = base/'codex'
            fake.write_text('#!/bin/sh\nexit 0\n')
            fake.chmod(0o755)
            env = dict(os.environ, PATH=str(base)+os.pathsep+os.environ['PATH'], WORKHARBOR_REPO_URL=str(project), WORKHARBOR_REPO_REVISION='a'*40, WORKHARBOR_WORKSPACE_ROOT=str(base/'issues'), WORKHARBOR_GITHUB_REPO='test-owner/test-repo', GITHUB_TOKEN='test-secret-never-write')
            env.pop('WORKHARBOR_FEATURE', None)
            env.pop('WORKHARBOR_ISSUE_BRANCH', None)
            subprocess.run(['python3', str(script)], env=env, check=True, capture_output=True)
            rendered = (project/'WORKFLOW.local.md').read_text()
            self.assertNotIn(env['GITHUB_TOKEN'], rendered)
            config = json.loads(rendered.split('---',2)[1])
            self.assertEqual('github', config['tracker']['kind'])
            self.assertEqual(['workharbor-ready'], config['tracker']['required_labels'])
            self.assertEqual(['open'], config['tracker']['active_states'])
            self.assertEqual(['closed'], config['tracker']['terminal_states'])
            self.assertEqual('$GITHUB_TOKEN', config['tracker']['provider']['token'])
            self.assertEqual('test-owner/test-repo', config['tracker']['provider']['repo'])
            self.assertEqual(str((base/'issues').resolve()), config['workspace']['root'])
            self.assertIn('git switch -c "work/$key" ' + 'a'*40, config['hooks']['after_create'])
            self.assertNotIn('001-workspace-lifecycle', config['hooks']['after_create'])
            env['WORKHARBOR_REPO_REVISION'] = 'main; echo unsafe'
            run = subprocess.run(['python3', str(script)], env=env, capture_output=True, text=True)
            self.assertNotEqual(0, run.returncode)
            self.assertIn('commit SHA', run.stderr)
            env['WORKHARBOR_REPO_REVISION'] = 'a'*40
            env['WORKHARBOR_GITHUB_REPO'] = 'https://github.com/test-owner/test-repo'
            run = subprocess.run(['python3', str(script)], env=env, capture_output=True, text=True)
            self.assertNotEqual(0, run.returncode)
            self.assertIn('owner/repository', run.stderr)
            env['WORKHARBOR_GITHUB_REPO'] = 'test-owner/test-repo'
            env['WORKHARBOR_WORKSPACE_ROOT'] = str(project/'nested')
            run = subprocess.run(['python3', str(script)], env=env, capture_output=True)
            self.assertNotEqual(0, run.returncode)

    def test_creation_hook_uses_pinned_nondefault_commit(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            project = base/'source'
            (project/'scripts').mkdir(parents=True)
            (project/'.specify').mkdir()
            (project/'.specify/README.md').write_text('Synthetic fixture\n')
            (project/'specs/demo').mkdir(parents=True)
            (project/'specs/demo/spec.md').write_text('Synthetic feature\n')
            (project/'.gitignore').write_text('.specify/feature.json\nWORKFLOW.local.md\n')
            (project/'WORKFLOW.md').write_text((ROOT/'WORKFLOW.md').read_text())
            for name in ('prepare_symphony.py', 'select_feature.py'):
                (project/'scripts'/name).write_bytes((ROOT/'scripts'/name).read_bytes())
            def git(*args):
                return subprocess.check_output(['git', *args], cwd=project, text=True).strip()
            git('init', '-b', 'main')
            git('add', '.')
            git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                '-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=/dev/null',
                'commit', '-m', 'test: pinned revision')
            pinned = git('rev-parse', 'HEAD')
            (project/'wrong-default.md').write_text('Not the approved tree\n')
            git('add', '.')
            git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                '-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=/dev/null',
                'commit', '-m', 'test: different default revision')
            fake = base/'codex'
            fake.write_text('#!/bin/sh\nexit 0\n')
            fake.chmod(0o755)
            env = dict(os.environ, PATH=str(base)+os.pathsep+os.environ['PATH'],
                       WORKHARBOR_REPO_URL=str(project), WORKHARBOR_REPO_REVISION=pinned,
                       WORKHARBOR_WORKSPACE_ROOT=str(base/'issues'),
                       WORKHARBOR_GITHUB_REPO='test-owner/test-repo', GITHUB_TOKEN='synthetic-secret',
                       WORKHARBOR_FEATURE='demo', WORKHARBOR_ISSUE_BRANCH='issue-2-t001-fixtures')
            subprocess.run(['python3', str(project/'scripts/prepare_symphony.py')], env=env,
                           check=True, capture_output=True)
            config = json.loads((project/'WORKFLOW.local.md').read_text().split('---',2)[1])
            checkout = base/'issues/issue-2'
            checkout.mkdir(parents=True)
            subprocess.run(['bash', '-c', config['hooks']['after_create']], cwd=checkout,
                           env=env, check=True, capture_output=True)
            self.assertEqual(pinned, subprocess.check_output(['git','rev-parse','HEAD'],cwd=checkout,text=True).strip())
            self.assertFalse((checkout/'wrong-default.md').exists())
            self.assertEqual('issue-2-t001-fixtures', subprocess.check_output(['git','branch','--show-current'],cwd=checkout,text=True).strip())
            self.assertEqual({'feature_directory':'specs/demo'}, json.loads((checkout/'.specify/feature.json').read_text()))
            self.assertEqual('', subprocess.check_output(['git','status','--porcelain'],cwd=checkout,text=True).strip())

    def test_retry_can_repair_failed_checks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            subprocess.run(['git', 'init', '-b', 'main', str(root)], check=True, capture_output=True)
            (root/'Makefile').write_text('check:\n\t@echo "broken link absent.md" >&2\n\t@exit 2\n')
            run = subprocess.run(['bash', str(ROOT/'scripts/symphony-before-run.sh')], cwd=root, capture_output=True, text=True)
            self.assertEqual(0, run.returncode, run.stderr)
            self.assertIn('broken link absent.md', run.stderr)
            self.assertIn('before handoff', run.stderr)
            branch = subprocess.check_output(['git', 'branch', '--show-current'], cwd=root, text=True).strip()
            self.assertTrue(branch.startswith('work/'))
            retry = subprocess.run(['bash', str(ROOT/'scripts/symphony-before-run.sh')], cwd=root, capture_output=True)
            self.assertEqual(0, retry.returncode)
            self.assertEqual(branch, subprocess.check_output(['git', 'branch', '--show-current'], cwd=root, text=True).strip())
