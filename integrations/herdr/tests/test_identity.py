# SPDX-License-Identifier: EUPL-1.2
import unittest
from identity import identity, index, valid_name, GLYPHS, TINTS, HOST_TITLE


class IdentityTests(unittest.TestCase):
    def test_identity_fnv1a_fixed_names(self):
        # Independently computed FNV-1a 32-bit reference outputs (not Python hash).
        for name, hashed in [('a', 0xe40c292c), ('hello', 0x4f9f2cab), ('foobar', 0xbf9cf968)]:
            with self.subTest(name=name):
                self.assertEqual(hashed % 5, index(name))
                self.assertEqual((GLYPHS[hashed % 5] + ' ws:' + name, TINTS[hashed % 5]), identity(name))

    def test_identity_never_uses_host_glyph(self):
        self.assertEqual('🟥 HOST', HOST_TITLE)
        self.assertEqual(('🔷', '🟠', '🟣', '🟡', '⬜'), GLYPHS)
        self.assertEqual(('#14204c', '#483024', '#180428', '#383000', '#184c44'), TINTS)
        self.assertNotIn('🟥', GLYPHS)
        self.assertEqual(5, len(set(GLYPHS)))

    def test_name_rule_exact_boundaries(self):
        for name in ['a', '0', 'my-ws', 'a' * 64]:
            self.assertTrue(valid_name(name))
        for name in ['', 'A', '-a', 'a-', 'a_b', 'a.b', 'a' * 65, 'a\n', 'a;true', 'a\x1b', None, 7]:
            self.assertFalse(valid_name(name))
            with self.assertRaises(ValueError):
                identity(name)
