// SPDX-License-Identifier: EUPL-1.2
// The pinned reference JSONC parser supplies syntax; this wrapper rejects ambiguity.
'use strict';
const fs = require('node:fs');
const { parseTree } = require('jsonc-parser');
function scalarString(value) {
  for (let i = 0; i < value.length; i++) {
    const c = value.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      const next = value.charCodeAt(++i);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (c >= 0xdc00 && c <= 0xdfff) return false;
  }
  return true;
}
function validate(node) {
  if (node.type === 'string' && !scalarString(node.value)) throw new Error();
  if (node.type === 'number' && !Number.isFinite(node.value)) throw new Error();
  if (node.type === 'object') {
    const names = new Set();
    for (const property of node.children || []) {
      const key = property.children[0].value;
      if (names.has(key)) throw new Error();
      names.add(key);
    }
  }
  for (const child of node.children || []) validate(child);
}
// Serialize validated AST nodes without converting number lexemes through JS Number.
function render(node, raw) {
  switch (node.type) {
    case 'number': return raw.slice(node.offset, node.offset + node.length);
    case 'object':
      return '{' + (node.children || []).map(property =>
        JSON.stringify(property.children[0].value) + ':' + render(property.children[1], raw)
      ).join(',') + '}';
    case 'array': return '[' + (node.children || []).map(child => render(child, raw)).join(',') + ']';
    case 'string': case 'boolean': case 'null': return JSON.stringify(node.value);
    default: throw new Error();
  }
}
try {
  if (!['strict', 'jsonc'].includes(process.argv[1]) || process.argv.length !== 2) throw new Error();
  const raw = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(fs.readFileSync(0));
  const strict = process.argv[1] === 'strict';
  const errors = [];
  const tree = parseTree(raw, errors, { allowTrailingComma: !strict, disallowComments: strict, allowEmptyContent: false });
  if (!tree || errors.length) throw new Error();
  validate(tree);
  process.stdout.write(render(tree, raw));
} catch (_) {
  process.stderr.write('invalid_configuration\n');
  process.exitCode = 1;
}
