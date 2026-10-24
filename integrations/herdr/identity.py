# SPDX-License-Identifier: EUPL-1.2
"""Fixed cosmetic identity from Feature 010 FR-021/021a; no terminal operations."""
import re

GLYPHS = ('🔷', '🟠', '🟣', '🟡', '⬜')
TINTS = ('#14204c', '#483024', '#180428', '#383000', '#184c44')
HOST_TITLE = '🟥 HOST'
HOST_TINT = '#3a0d0d'


def valid_name(name):
    return isinstance(name, str) and re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?', name) is not None


def index(name):
    value = 2166136261
    for byte in name.encode('utf-8'):
        value = ((value ^ byte) * 16777619) & 0xffffffff
    return value % len(GLYPHS)


def identity(name):
    if not valid_name(name):
        raise ValueError('invalid workspace name')
    i = index(name)
    return GLYPHS[i] + ' ws:' + name, TINTS[i]
