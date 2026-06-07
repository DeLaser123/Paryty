/**
 * Red-team proof for PARYTY-DS-NAV-TEXT-001 evidence integrity.
 *
 * Verifies that the scoped manifest/handoff entries answer the required
 * UX-evidence prompts and use an allowed evidence-bundle classification.
 */

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '../../../..');

const manifest = readFileSync(
  resolve(repoRoot, 'agent_manifests/frontend-engineer/frontend-engineer_manifest.md'),
  'utf8',
);
const handoff = readFileSync(
  resolve(repoRoot, 'agent_manifests/frontend-engineer/frontend-engineer_handoff.md'),
  'utf8',
);

const scopedEntries = [
  { id: 'MF-20260525-010', source: manifest },
  { id: 'MF-20260525-009', source: manifest },
  { id: 'HO-20260525-009', source: handoff },
];

const requiredSignals = [
  ['UX Evidence Block', /### UX Evidence Block/i],
  ['canonical journey', /canonical journey/i],
  ['user-visible improvement', /user-visible/i],
  ['non-UI evidence', /non-ui evidence/i],
  ['layer gate', /layer gate/i],
  ['remaining journey work', /remains/i],
  ['classification', /classification/i],
];

const allowedClassifications = /\*\*Classification:\*\*\s*(product[-_]facing|infrastructure[-_]only|readiness[-_]gate|future[-_]scope)\b/i;

function entryBlock(source, id) {
  const marker = `## ${id}`;
  const start = source.indexOf(marker);
  if (start < 0) throw new Error(`${id}: entry not found`);
  const next = source.indexOf('\n---\n', start);
  return source.slice(start, next < 0 ? undefined : next);
}

const failures = [];

for (const { id, source } of scopedEntries) {
  const block = entryBlock(source, id);
  for (const [label, pattern] of requiredSignals) {
    if (!pattern.test(block)) failures.push(`${id}: missing ${label}`);
  }
  if (!allowedClassifications.test(block)) {
    failures.push(`${id}: classification must be product-facing, infrastructure-only, readiness-gate, or future-scope`);
  }
}

if (failures.length) {
  console.error('PARYTY-DS-NAV-TEXT-001 evidence bundle failures:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log('PARYTY-DS-NAV-TEXT-001 evidence bundle check passed.');