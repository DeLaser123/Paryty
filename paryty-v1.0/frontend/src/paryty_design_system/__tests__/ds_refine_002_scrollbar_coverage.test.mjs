import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, '../design-system-page.css'), 'utf8');

const ownedScrollableSelectors = [
  '.ds-sidebar__nav',
  '.ds-preview-sidebar .ds-sidebar__nav',
  '.ds-preview-body .aef-container-card__body',
  '.ds-preview-right-col',
  '.ds-dashboard-outer',
  '.aef-scroll-region',
  '.aef-scroll-region--h',
  '.aef-modal-body',
  '.aef-cmd-results',
];

const expectedScrollbarColor = 'scrollbar-color: rgba(255, 255, 255, 0.14) transparent';
const missing = ownedScrollableSelectors.filter(selector => {
  const selectorIndex = css.indexOf(selector);
  if (selectorIndex === -1) return true;
  const blockStart = css.indexOf('{', selectorIndex);
  const blockEnd = css.indexOf('}', blockStart);
  const block = css.slice(blockStart + 1, blockEnd);
  return !block.includes(expectedScrollbarColor);
});

if (missing.length > 0) {
  throw new Error(
    `PARYTY-DS-REFINE-002 scrollbar coverage failed. Missing slim translucent scrollbar-color on: ${missing.join(', ')}`,
  );
}

console.log('PARYTY-DS-REFINE-002 scrollbar coverage passed.');