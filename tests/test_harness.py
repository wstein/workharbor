import importlib.util
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location('checks', Path(__file__).resolve().parents[1] / 'scripts/check_repo.py')
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)

class HarnessTests(unittest.TestCase):
    def test_broken_document_link_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            doc = root/'entry.md'
            doc.write_text('[missing](absent.md)')
            self.assertIn('broken link', checks.document_errors(root, doc)[0])
            (root/'absent.md').write_text('present')
            self.assertEqual([], checks.document_errors(root, doc))

    def test_workspace_path_traversal_rejected(self):
        base = dict(schema_version=1, devcontainer_path='.devcontainer/devcontainer.json', projects_path='projects', bundles=[])
        self.assertEqual([], checks.workspace_errors(base))
        for unsafe in ('../other', '/etc', 'projects/../../other', 'projects\\other'):
            with self.subTest(path=unsafe):
                self.assertTrue(checks.workspace_errors(dict(base, projects_path=unsafe)))

    def test_provider_dependency_rejected_outside_adapter(self):
        text = 'import "github.com/docker/docker/client"'
        self.assertTrue(checks.boundary_errors(Path('apps/host_agent/internal/service.go'), text))
        self.assertEqual([], checks.boundary_errors(Path('apps/host_agent/internal/providers/docker/provider.go'), text))
        self.assertTrue(checks.boundary_errors(Path('apps/control_plane/lib/service.ex'), 'DockerProvider'))

    def test_development_orchestrator_rejected_in_product(self):
        self.assertTrue(checks.boundary_errors(Path('apps/control_plane/lib/service.ex'), 'codex app-server'))

if __name__ == '__main__':
    unittest.main()
