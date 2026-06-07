import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const repoRoot = resolve(fileURLToPath(import.meta.url), '../../../../..');

const files = [
  {
    path: 'agent_manifests/frontend-engineer/frontend-engineer_manifest.md',
    entries: ['MF-20260525-006', 'MF-20260525-005'],
  },
  {
    path: 'agent_manifests/frontend-engineer/frontend-engineer_handoff.md',
    entries: ['HO-20260525-005', 'HO-20260525-004'],
  },
];

const requiredSignals = [
  /UX Evidence Block/i,
  /canonical journey/i,
  /user-visible/i,
  /non-UI evidence/i,
  /layer gate/i,
  /remains/i,
  /product-facing|infrastructure-only|future-scope|readiness gate/i,
];

const failures = [];

for (const file of files) {
  const text = readFileSync(resolve(repoRoot, file.path), 'utf8');
  for (const entry of file.entries) {
    const start = text.indexOf(entry);
    if (start === -1) {
      failures.push(`${file.path}: missing ${entry}`);
      continue;
    }

    const nextMarker = text.indexOf('<!-- END ', start);
    const end = nextMarker === -1 ? text.length : text.indexOf('-->', nextMarker) + 3;
    const block = text.slice(start, end);
    const missing = requiredSignals.filter(pattern => !pattern.test(block));
    if (missing.length > 0) {
      failures.push(`${file.path}:${entry} missing UX evidence signals: ${missing.map(String).join(', ')}`);
    }
  }
}

if (failures.length > 0) {
  throw new Error(`PARYTY-DS-REFINE-002 evidence bundle check failed. ${failures.join(' | ')}`);
}

console.log('PARYTY-DS-REFINE-002 evidence bundle check passed.');