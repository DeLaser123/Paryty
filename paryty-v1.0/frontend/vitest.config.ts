import { defineConfig } from 'vitest/config';

// Vitest is scoped to the typed unit-test suite under src/__tests__/.
//
// The design-system standalone scripts (src/paryty_design_system/__tests__/*.test.mjs)
// are plain Node.js scripts that throw on failure — not Vitest describe/it suites.
// They are excluded here and run via `npm run test:ds` instead, which invokes them
// directly with node so each one reports its own pass/fail to stdout.
//
// Two of those scripts (ds_nav_foreground_color, ds_nav_selector_geometry) require
// puppeteer (browser binary) and are noted as needing `npm install puppeteer` before
// they can pass.  The two evidence-bundle scripts require agent manifest files in
// agent_manifests/frontend-engineer/ (process documentation, not product code).
export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/__tests__/**/*.test.ts'],
  },
});
