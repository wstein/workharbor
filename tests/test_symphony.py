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
            env = dict(os.environ, PATH=str(base)+os.pathsep+os.environ['PATH'], WORKHARBOR_REPO_URL=str(project), WORKHARBOR_WORKSPACE_ROOT=str(base/'issues'), WORKHARBOR_GITHUB_REPO='test-owner/test-repo', GITHUB_TOKEN='test-secret-never-write')
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
            env['WORKHARBOR_GITHUB_REPO'] = 'https://github.com/test-owner/test-repo'
            run = subprocess.run(['python3', str(script)], env=env, capture_output=True, text=True)
            self.assertNotEqual(0, run.returncode)
            self.assertIn('owner/repository', run.stderr)
            env['WORKHARBOR_GITHUB_REPO'] = 'test-owner/test-repo'
            env['WORKHARBOR_WORKSPACE_ROOT'] = str(project/'nested')
            run = subprocess.run(['python3', str(script)], env=env, capture_output=True)
            self.assertNotEqual(0, run.returncode)

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
