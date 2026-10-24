#!/usr/bin/env python3
# SPDX-License-Identifier: EUPL-1.2
"""Standalone optional plugin checks. Never called by required repository CI."""
from pathlib import Path
import sys
import unittest
from check_palette import main as check_palette


def main():
    suite = unittest.defaultTestLoader.discover(str(Path(__file__).resolve().parent / 'tests'))
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    if not result.wasSuccessful() or not result.testsRun or result.skipped:
        return 1
    return check_palette()


if __name__ == '__main__':
    sys.exit(main())
