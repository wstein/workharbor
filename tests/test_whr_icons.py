import importlib.util
import os
import re
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path

ICONS = Path(os.environ.get('WHR_ICONS_DIR') or Path(__file__).resolve().parents[1] / 'assets/icons/whr')
SPEC = importlib.util.spec_from_file_location('whr_icon_generator', Path(__file__).resolve().parents[1] / 'assets/icons/whr/generate.py')
GEN = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GEN)

SVG = '{http://www.w3.org/2000/svg}'
ROLES = ('dev', 'test', 'build')
DISTROS = ('debian', 'ubuntu', 'alpine')
BADGE_FILL = {'debian': '#d70a53', 'ubuntu': '#e95420', 'alpine': '#0d597f', 'arcane': '#7c3aed'}
BADGE_LETTER_PATH = {
    'debian': 'M43.5 41.5V54.5H47Q53 54.5 53 48Q53 41.5 47 41.5Z',
    'ubuntu': 'M43.5 41.5V49Q43.5 55 48.5 55Q53.5 55 53.5 49V41.5',
    'alpine': 'M43 55L48 41L53 55M45 50.5H51',
    'arcane': 'M43.5 42L53.5 54M53.5 42L43.5 54',
}
ROLE_TINT = {'dev': '#14b8a6', 'test': '#f59e0b', 'build': '#7c8da5'}
FORBIDDEN = ('<!doctype', '<!entity', '<script', '<image', '<foreignobject', 'href=', 'http://', 'https://', '<style', '@import', 'url(', '<text')
ELEMENTS = {'svg', 'title', 'g', 'path', 'circle'}
ATTRIBUTES = {'width', 'height', 'viewBox', 'd', 'cx', 'cy', 'r', 'fill', 'stroke', 'stroke-width', 'stroke-linecap',
              'stroke-linejoin'}


def expected_names():
    names = ['whr.svg', 'whrx-arcane.svg']
    names += [f'whr-{role}-{distro}.svg' for role in ROLES for distro in DISTROS]
    return sorted(names)


def badge_distro(name):
    if name == 'whrx-arcane.svg':
        return 'arcane'
    return name.removesuffix('.svg').split('-')[2] if name.startswith('whr-') and name != 'whr.svg' else None


class IconTests(unittest.TestCase):
    def test_full_matrix_exists_and_nothing_else_is_svg(self):
        self.assertEqual(expected_names(), sorted(p.name for p in ICONS.glob('*.svg')))

    def test_files_are_wellformed_xml_with_required_parts(self):
        for name in expected_names():
            with self.subTest(name):
                root = ET.parse(ICONS / name).getroot()
                self.assertEqual(SVG + 'svg', root.tag)
                self.assertTrue(root.get('viewBox'), 'viewBox missing')
                self.assertEqual('0 0 64 64', root.get('viewBox'))
                self.assertTrue(root.get('width') and root.get('height'), 'width/height missing')
                title = root.find(SVG + 'title')
                self.assertIsNotNone(title, 'title missing')
                self.assertTrue((title.text or '').strip(), 'title empty')

    def test_no_script_image_foreign_object_or_external_reference(self):
        for name in expected_names():
            with self.subTest(name):
                text = (ICONS / name).read_text(encoding='utf-8')
                for token in FORBIDDEN:
                    if token == 'http://':
                        text = text.replace('xmlns="http://www.w3.org/2000/svg"', '')
                    self.assertNotIn(token, text.lower())

    def test_only_allowed_elements_and_attributes(self):
        for name in expected_names():
            with self.subTest(name):
                for element in ET.parse(ICONS / name).getroot().iter():
                    self.assertIn(element.tag.removeprefix(SVG), ELEMENTS)
                    self.assertTrue(element.tag.startswith(SVG), element.tag)
                    for attribute in element.attrib:
                        self.assertIn(attribute, ATTRIBUTES)

    def test_size_limit(self):
        for name in expected_names():
            with self.subTest(name):
                self.assertLessEqual((ICONS / name).stat().st_size, 2048)

    def test_badge_color_and_letter_per_distro(self):
        for name in expected_names():
            distro = badge_distro(name)
            with self.subTest(name):
                text = (ICONS / name).read_text(encoding='utf-8')
                if distro is None:
                    self.assertNotIn('<circle cx="48"', text)
                    continue
                self.assertIn(f'<circle cx="48" cy="48" r="13" fill="{BADGE_FILL[distro]}"', text)
                self.assertIn(f'<path d="{BADGE_LETTER_PATH[distro]}"', text)
                self.assertIn('stroke="#ffffff"', text)

    def test_role_tint_and_no_black_or_white_glyph(self):
        for name in expected_names():
            with self.subTest(name):
                text = (ICONS / name).read_text(encoding='utf-8')
                m = re.fullmatch(r'whr-(dev|test|build)-[a-z]+\.svg', name)
                if m:
                    self.assertIn(f'stroke="{ROLE_TINT[m.group(1)]}"', text)
                glyph = text.split('<circle cx="48"')[0]
                self.assertNotRegex(glyph.lower(), r'#(000000|000|ffffff|fff)\b|"(black|white)"')

    def test_regeneration_is_byte_identical(self):
        with tempfile.TemporaryDirectory() as tmp:
            GEN.main(['generate.py', tmp])
            self.assertEqual(expected_names(), sorted(p.name for p in Path(tmp).iterdir()))
            for name in expected_names():
                with self.subTest(name):
                    self.assertEqual((Path(tmp) / name).read_bytes(), (ICONS / name).read_bytes())


if __name__ == '__main__':
    unittest.main()
