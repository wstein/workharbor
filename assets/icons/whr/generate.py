#!/usr/bin/env python3
"""Write the whr container icons (SVG) from the tables below.

Usage: python3 generate.py [output-directory]   (default: the directory of this script)

The anchor glyph is a placeholder; replace GLYPH to change every icon. Output is deterministic.
"""
import sys
from pathlib import Path

OUTLINE = '#0f1720'
BASE_TINT = '#3b82f6'

ROLES = {'dev': '#14b8a6', 'test': '#f59e0b', 'build': '#7c8da5'}

# Badge: fill color and the letter (D, U, A, X) as a stroked path (white), centered on (48, 48).
BADGES = {
    'debian': ('#d70a53', 'M43.5 41.5V54.5H47Q53 54.5 53 48Q53 41.5 47 41.5Z'),
    'ubuntu': ('#e95420', 'M43.5 41.5V49Q43.5 55 48.5 55Q53.5 55 53.5 49V41.5'),
    'alpine': ('#0d597f', 'M43 55L48 41L53 55M45 50.5H51'),
    'arcane': ('#7c3aed', 'M43.5 42L53.5 54M53.5 42L43.5 54'),
}

# The single place that draws the glyph: an anchor, outline first, then the tinted stroke.
GLYPH_PATHS = (
    'M28 16V54',
    'M19 23H37',
    'M11 37C13 49 21 54 28 54C35 54 43 49 45 37',
)
GLYPH_RING = (28, 11, 5)


def glyph(tint):
    cx, cy, r = GLYPH_RING
    outline = ''.join(f'<path d="{d}"/>' for d in GLYPH_PATHS)
    return (
        f'<g fill="none" stroke-linecap="round" stroke-linejoin="round">'
        f'<g stroke="{OUTLINE}" stroke-width="9">{outline}<circle cx="{cx}" cy="{cy}" r="{r}"/></g>'
        f'<g stroke="{tint}" stroke-width="5">{outline}<circle cx="{cx}" cy="{cy}" r="{r}"/></g>'
        f'</g>'
    )


def badge(distro):
    fill, path = BADGES[distro]
    return (
        f'<circle cx="48" cy="48" r="13" fill="{fill}" stroke="{OUTLINE}" stroke-width="2"/>'
        f'<path d="{path}" fill="none" stroke="#ffffff" stroke-width="2.8" '
        f'stroke-linecap="round" stroke-linejoin="round"/>'
    )


def svg(title, tint, distro=None):
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">'
        f'<title>{title}</title>{glyph(tint)}{badge(distro) if distro else ""}</svg>\n'
    )


def files():
    """Map file name to content."""
    out = {'whr.svg': svg('whr', BASE_TINT)}
    for role, tint in ROLES.items():
        for distro in ('debian', 'ubuntu', 'alpine'):
            out[f'whr-{role}-{distro}.svg'] = svg(f'whr {role} {distro}', tint, distro)
    out['whrx-arcane.svg'] = svg('whrx arcane', BASE_TINT, 'arcane')
    return out


def main(argv):
    target = Path(argv[1]) if len(argv) > 1 else Path(__file__).resolve().parent
    target.mkdir(parents=True, exist_ok=True)
    for name, content in files().items():
        (target / name).write_bytes(content.encode('utf-8'))


if __name__ == '__main__':
    main(sys.argv)
