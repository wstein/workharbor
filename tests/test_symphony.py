import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

class SymphonyTests(unittest.TestCase):
    def test_prepare_fails_without_environment(self):
        env = {k:v for k,v in os.environ.items() if not k.startswith('ANCHORAGE_') and k != 'LINEAR_API_KEY'}
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
            env = dict(os.environ, PATH=str(base)+os.pathsep+os.environ['PATH'], ANCHORAGE_REPO_URL=str(project), ANCHORAGE_WORKSPACE_ROOT=str(base/'issues'), ANCHORAGE_LINEAR_PROJECT='test-project', LINEAR_API_KEY='test-secret-never-write')
            subprocess.run(['python3', str(script)], env=env, check=True, capture_output=True)
            rendered = (project/'WORKFLOW.local.md').read_text()
            self.assertNotIn(env['LINEAR_API_KEY'], rendered)
            config = json.loads(rendered.split('---',2)[1])
            self.assertEqual('$LINEAR_API_KEY', config['tracker']['provider']['api_key'])
            self.assertEqual('test-project', config['tracker']['provider']['project_slug'])
            self.assertEqual(str(base/'issues'), config['workspace']['root'])
            env['ANCHORAGE_WORKSPACE_ROOT'] = str(project/'nested')
            run = subprocess.run(['python3', str(script)], env=env, capture_output=True)
            self.assertNotEqual(0, run.returncode)
