#!/usr/bin/env python3
"""Select a Spec Kit feature for this checkout without modifying shared state."""
import argparse
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('feature', help='Feature directory name under specs/')
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
feature = (root/'specs'/args.feature).resolve()
if feature.parent != (root/'specs').resolve() or not (feature/'spec.md').is_file():
    parser.error('Choose an existing direct specs/ directory containing spec.md.')
(root/'.specify/feature.json').write_text(json.dumps({'feature_directory':str(feature.relative_to(root))}) + '\n')
print('Selected ' + args.feature + ' for this checkout (ignored local state).')
