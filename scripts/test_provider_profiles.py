"""Feature 005 T002: inert parsing, identities and action requirements only."""
from pathlib import Path
import hashlib
import os
import subprocess
import sys
import tempfile
import unittest

# Isolated Python omits the script directory. Pin only this trusted adjacent path;
# do not consult cwd, PYTHONPATH, entry points or repository-selected plugins.
_SCRIPT_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(_SCRIPT_DIR))
import provider_profiles as profiles


class T002Tests(unittest.TestCase):
    CANONICAL_ROWS = {
        'begin': ('execution', 'entry', 'begin', ('bridge.begin',), 'entry'),
        'inspect': ('observation', None, 'inspect', ('bridge.inspect',), None),
        'verify': ('execution', 'completion', 'verify', ('bridge.verify',), 'completion'),
        'complete': ('execution', 'completion', 'complete', ('bridge.complete',), 'completion'),
        'record-commit': ('execution', 'completion', 'record-commit', ('bridge.record-commit',), 'completion'),
        'resume': ('execution', 'resume', 'resume', ('bridge.resume',), 'resume'),
        'rework': ('execution', 'entry', 'rework', ('bridge.rework',), 'entry'),
        'status': ('observation', None, None, ('ledger.read',), None),
        'lookup': ('observation', None, None, ('bridge.lookup',), None),
        'transfer': ('execution', 'eligibility', None, ('claim.fenced-transfer',), None),
        'ready': ('execution', 'eligibility', None, (), 'ready'),
        'publish': ('execution', 'handoff', None, (), 'handoff'),
        'final-labels': ('execution', 'handoff', None, (), 'handoff'),
        'provider-execute': ('execution', 'provider', None, ('provider.execution',), None),
        'safe-stop': ('safety', None, None, ('runtime.safe-stop',), None),
    }

    def rejected(self, callback, *args, **kwargs):
        with self.assertRaises(profiles.ProfileError) as caught:
            callback(*args, **kwargs)
        self.assertNotIn('secret-canary', str(caught.exception))
        self.assertEqual(str(caught.exception), caught.exception.code)

    def test_strict_json(self):
        self.assertEqual({'name': 'é', 'nested': [True, None, -2]},
                         profiles.parse_json('{"name":"é","nested":[true,null,-2]}'.encode()))
        invalid = [b'{"a":1,"a":2}', b'{"nested":[{"a":1,"a":2}]}',
                   b'1.0', b'1e0', b'NaN', b'Infinity', b'-Infinity',
                   b'{"a":[1.1]}', b'"\xff"', b'"\\ud800"', b'"\\udfff"',
                   b'\xef\xbb\xbf{}', b'', b'{} {}', b'{secret-canary}',
                   b'[' * 2000 + b'0' + b']' * 2000, '{}', None, bytearray(b'{}')]
        for value in invalid:
            with self.subTest(value=repr(value)[:80]):
                self.rejected(profiles.parse_json, value)
        value = profiles.parse_json(b'{"required":1,"optional":true,"nested":{"x":2}}')
        self.assertIs(value, profiles.require_keys(value, {'required', 'nested'}, {'optional'}))
        self.assertIs(value['nested'], profiles.require_keys(value['nested'], {'x'}))
        for invalid in ({}, {'required': 1, 'secret-canary': True}, [], None):
            self.rejected(profiles.require_keys, invalid, {'required'})
        self.rejected(profiles.require_keys, value['nested'], {'other'})
        for invalid in (True, False, 1.0, '1', None):
            self.rejected(profiles.require_integer, invalid)
            self.rejected(profiles.require_timestamp, invalid)
        for epoch in (-1, 0, 2000):
            self.assertEqual(epoch, profiles.require_timestamp(epoch))
        self.assertEqual((1000, 3000), profiles.require_interval(1000, 3000))
        for start, end in ((1, 1), (2, 1), (True, 2), (1, False), (1, float('inf'))):
            self.rejected(profiles.require_interval, start, end)

    def test_canonical_identity(self):
        contract = (_SCRIPT_DIR.parent / 'specs/005-provider-aware-orchestration/contracts/task-acceptance.md').read_text()
        import re
        vectors = re.findall(r'### V(0[1-7])\n\n```json\n([^\n]+)\n```\n\nSHA-256: `([a-f0-9]{64})`', contract)
        self.assertEqual(7, len(vectors))
        values = {}
        for number, text, digest in vectors:
            with self.subTest(vector=number):
                value = profiles.parse_json(text.encode())
                values[number] = value
                self.assertEqual(text.encode(), profiles.canonical_bytes(value))
                self.assertEqual(digest, profiles.canonical_digest(value))
                self.assertEqual(digest, profiles.require_digest(digest))
                self.assertNotEqual(digest, hashlib.sha256(text.encode() + b'\n').hexdigest())
                if type(value) is dict:
                    self.assertEqual(digest, profiles.canonical_digest(dict(reversed(list(value.items())))))
        self.assertNotEqual(profiles.canonical_digest(values['02']), profiles.canonical_digest(values['03']))
        basis = {key: value for key, value in values['04'].items()
                 if key not in ('policy_digest', 'capability_digest')}
        self.assertEqual(values['05'], basis)
        self.assertNotEqual(profiles.canonical_digest(values['04']), profiles.canonical_digest(values['06']))
        context = profiles.parse_json(profiles.canonical_bytes(values['07']))
        approved = context['approval'].pop('context_digest')
        self.assertEqual(approved, profiles.canonical_digest(context))
        self.assertNotEqual(approved, profiles.canonical_digest(values['07']))
        cyclic = []
        cyclic.append(cyclic)
        for value in (1.0, float('nan'), {'a': [float('inf')]}, {'a': '\ud800'},
                      {1: 'secret-canary'}, ('tuple',), object(), cyclic):
            self.rejected(profiles.canonical_bytes, value)
        for digest in ('f' * 63, 'f' * 65, 'F' * 64, 'sha256:' + 'f' * 64, None, 1):
            self.rejected(profiles.require_digest, digest)

    def test_input_limits(self):
        self.assertEqual({}, profiles.parse_json(b'{}' + b' ' * (1048576 - 2)))
        self.rejected(profiles.parse_json, b'{}' + b' ' * (1048576 - 1))
        for check in (profiles.require_profiles, profiles.require_evidence_refs):
            values = [{} for _ in range(100)]
            self.assertIs(values, check(values))
            self.assertEqual(100, len(values))
            self.rejected(check, values + [{}])
            self.rejected(check, tuple(values))
        for check, limit in ((profiles.require_event_count, 1000),
                             (profiles.require_database_size, 16 * 1024 * 1024)):
            self.assertEqual(limit, check(limit))
            self.assertEqual(0, check(0))
            for invalid in (limit + 1, -1, True, 1.0):
                self.rejected(check, invalid)
        for value in ('a', 'a' * 128, 'A0._:-'):
            self.assertEqual(value, profiles.require_identifier(value))
        for value in ('', 'a' * 129, 'ä', 'a/b', 'a\\b', '*', ' secret-canary', 1, None):
            self.rejected(profiles.require_identifier, value)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / 'nested').mkdir()
            artifact = root / 'nested/file.json'
            artifact.write_bytes(b'{"ok":true}')
            self.assertEqual('nested/file.json', profiles.require_artifact_path('nested/file.json'))
            self.assertEqual(b'{"ok":true}', profiles.read_artifact(root, 'nested/file.json'))
            for path in ('', '.', '..', '../outside', '/absolute', './nested/file.json',
                         'nested/../nested/file.json', 'nested//file.json', 'nested/file.json/',
                         'nested\\file.json', 'C:/file', 'file\x00name', 1, None):
                self.rejected(profiles.require_artifact_path, path)
            (root / 'alias').symlink_to(root / 'nested', target_is_directory=True)
            (root / 'link').symlink_to(artifact)
            (root / 'outside').symlink_to(root.parent, target_is_directory=True)
            os.link(artifact, root / 'hardlink')
            os.mkfifo(root / 'fifo')
            for path in ('nested', 'absent', 'alias/file.json', 'link', 'outside/file', 'hardlink', 'fifo'):
                self.rejected(profiles.read_artifact, root, path)
            self.rejected(profiles.read_artifact, root / 'alias', 'file.json')
            # The source is now hardlinked too: reject both aliases, not just the new name.
            self.rejected(profiles.read_artifact, root, 'nested/file.json')
            (root / 'large').write_bytes(b'x' * 1048576)
            self.assertEqual(1048576, len(profiles.read_artifact(root, 'large')))
            (root / 'large').write_bytes(b'x' * 1048577)
            self.rejected(profiles.read_artifact, root, 'large')

    def test_contract_action_table(self):
        # Parse the contract's action table at test time and compare it with the
        # frozen rows; any shape change in the contract fails loudly.
        import re
        text = (_SCRIPT_DIR.parent / 'specs/005-provider-aware-orchestration/contracts/cli-and-host.md').read_text()
        header = '| Action | Class / guard boundary | Required capability and delegation |'
        self.assertEqual(1, text.count(header))
        lines = text[text.index(header):].split('\n')
        self.assertEqual('| --- | --- | --- |', lines[1])
        body = []
        for line in lines[2:]:
            if not line.startswith('|'):
                break
            body.append(line)
        self.assertEqual(15, len(body))
        vocab = re.search(r'guard --operation accepts only ([a-z, /-]+) and maps', text.replace('\n', ' '))
        self.assertIsNotNone(vocab)
        guard_ops = tuple(re.findall(r'[a-z]+(?:-[a-z]+)*', vocab.group(1)))
        self.assertEqual(7, len(guard_ops))
        parsed = {}
        for line in body:
            cells = [cell.strip() for cell in line.strip().strip('|').split(' | ')]
            self.assertEqual(3, len(cells), line)
            action, klass, delegation = cells
            self.assertRegex(action, r'^[a-z][a-z-]*$')
            self.assertNotIn(action, parsed)
            match = re.fullmatch(r'(Execution|Observation|Safety)(?: / ([a-z]+))?', klass)
            self.assertIsNotNone(match, klass)
            # Contract: guard operations map one-to-one to the same action token.
            operation = action if action in guard_ops and 'existing guard' in delegation else None
            guard = re.search(r'existing guard (\w[\w-]*)', delegation)
            if guard and guard.group(1) in guard_ops:
                self.assertEqual(action, guard.group(1))
            package = re.search(r'\bG\(([a-z]+),\s*([^)]+)\)', delegation)
            if package:
                mode = package.group(2).strip()
                self.assertTrue(mode == 'm' or mode == 'owner-authorized rework', mode)
                self.assertEqual('owner-authorized rework' if mode != 'm' else 'implement',
                                 profiles.action_requirements(action, 'implement').package_mode)
                self.assertEqual(package.group(1), profiles.action_spec(action).package_action)
            # status adds bridge.status only when a guard ID is retained (separate test).
            caps = re.sub(r'bridge\.status when a guard ID is retained', '', delegation)
            caps = re.sub(r'\bG\([^)]*\)', '', caps)
            # Any dotted lowercase token is a capability; unknown ones fail the comparison.
            caps = tuple(re.findall(r'\b[a-z]+\.[a-z][a-z.-]*[a-z]\b', caps))
            parsed[action] = (match.group(1).lower(), match.group(2), operation, caps,
                              package.group(1) if package else None)
        self.assertEqual(self.CANONICAL_ROWS, parsed)

    def test_canonical_actions(self):
        # Independently freeze literals from the accepted table; G is deferred,
        # never copied or treated as an authorization/capability observation.
        rows = self.CANONICAL_ROWS
        self.assertEqual(set(rows), set(profiles.ACTIONS))
        with self.assertRaises(TypeError):
            profiles.ACTIONS['begin'] = profiles.ACTIONS['inspect']
        with self.assertRaises(AttributeError):
            profiles.ACTIONS['begin'].boundary = 'handoff'
        for action, expected in rows.items():
            with self.subTest(action=action):
                spec = profiles.action_spec(action)
                self.assertEqual(expected, (spec.action_class, spec.boundary, spec.guard_operation,
                                            spec.capabilities, spec.package_action))
                self.assertEqual(action, profiles.require_actions([action])[0])
                for mode in profiles.TASK_MODES:
                    required = profiles.action_requirements(action, mode)
                    self.assertEqual(spec.package_action, required.package_action)
                    self.assertEqual('owner-authorized rework' if action == 'rework' else mode,
                                     required.package_mode)
                    self.assertEqual(spec.capabilities, required.capabilities)
                    self.assertEqual(action == 'transfer', required.downstream_transfer_required)
                    self.assertEqual(action not in ('provider-execute', 'safe-stop'), required.reference_executor_available)
                if spec.guard_operation:
                    self.assertEqual(action, profiles.action_for_operation(action))
                else:
                    self.rejected(profiles.action_for_operation, action)
                if spec.action_class == 'execution':
                    self.assertEqual((action,), profiles.require_actions([action], execution_only=True))
                else:
                    self.rejected(profiles.require_actions, [action], execution_only=True)
        self.assertEqual(('bridge.status', 'ledger.read'),
                         profiles.action_requirements('status', 'implement', guard_attempt_retained=True).capabilities)
        self.assertEqual('lookup', profiles.action_for_command('lookup_creation'))
        self.assertEqual('status', profiles.action_for_command('status'))
        self.assertEqual('transfer', profiles.action_for_command('transfer'))
        self.assertEqual('begin', profiles.action_for_command('guard', operation='begin'))
        for action in ('entry', 'completion', '*', 'BEGIN', 'assess', 'exercise', 'secret-canary', None, []):
            self.rejected(profiles.action_spec, action)
        for actions in (['begin', 'begin'], ['*'], 'begin', [], ['inspect', 'BEGIN']):
            self.rejected(profiles.require_actions, actions)
        for mode in ('rework', 'orchestrated', None, True):
            self.rejected(profiles.action_requirements, 'begin', mode)
        self.rejected(profiles.action_requirements, 'status', 'implement', guard_attempt_retained=1)
        for command in ('assess', 'exercise', 'ready', 'publish', 'final-labels', 'secret-canary'):
            self.rejected(profiles.action_for_command, command)
        self.rejected(profiles.action_for_command, 'status', operation='begin')
        # An input-selected module cannot shadow the trusted adjacent import under -I.
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, 'provider_profiles.py').write_text('raise RuntimeError("plugin executed")\n')
            environment = dict(os.environ, PYTHONPATH=directory)
            result = subprocess.run([sys.executable, '-I', '-B', str(Path(__file__).resolve()),
                                     'T002Tests.test_strict_json', '-q'], cwd=directory,
                                    env=environment, capture_output=True, timeout=30)
            self.assertEqual(0, result.returncode, result.stderr.decode())


if __name__ == '__main__':
    unittest.main()
