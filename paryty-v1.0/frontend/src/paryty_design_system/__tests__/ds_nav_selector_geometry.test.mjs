/**
 * ds_nav_selector_geometry.test.mjs
 *
 * Puppeteer DOM verification for AnimatedNavSection selector geometry:
 *  - Expanded: pill selector width > NAV_ITEM_H, border-radius indicates pill (not circle-only).
 *  - Collapsed: selector width ~= NAV_ITEM_H, border-radius is 50% (circular).
 *  - No horizontal overflow at viewport widths 390 and 1440.
 *
 * Runs against `vite preview` serving ui/dist (build must be current).
 * Launched by: node ui/src/paryty_design_system/__tests__/ds_nav_selector_geometry.test.mjs
 */

import puppeteer from 'puppeteer';
import { execSync, spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const uiRoot = resolve(here, '../../../'); // ui/

const PREVIEW_PORT = 4179;
const BASE_URL     = `http://localhost:${PREVIEW_PORT}`;
// Must match AnimatedNavSection constant in DesignSystemPage.tsx
const NAV_ITEM_H   = 34;

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------
function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

/** Start vite preview; resolve when server is ready. */
async function startPreviewServer() {
  // Use the vite binary directly to avoid npm.cmd spawn issues on Windows.
  const viteBin = resolve(uiRoot, 'node_modules/vite/bin/vite.js');
  const srv = spawn(
    process.execPath,
    [viteBin, 'preview', '--port', String(PREVIEW_PORT), '--strictPort'],
    { cwd: uiRoot, stdio: 'ignore', shell: false, detached: false },
  );
  // Wait for the server to bind: poll HTTP until it responds.
  // setInterval with async callback is reliable in CJS but can be tricky
  // in ES module top-level-await context; use a recursive setTimeout instead.
  await new Promise((res, rej) => {
    const deadline = Date.now() + 20000;
    srv.on('error', rej);
    function poll() {
      fetch(`http://localhost:${PREVIEW_PORT}/`)
        .then(r => { if (r.status < 500) { res(); } else { scheduleRetry(); } })
        .catch(() => scheduleRetry());
    }
    function scheduleRetry() {
      if (Date.now() > deadline) { rej(new Error('Preview server did not start in 20s')); return; }
      setTimeout(poll, 400);
    }
    setTimeout(poll, 800); // initial delay
  });
  return srv;
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------
let server;
let browser;
let failed = false;

try {
  server  = await startPreviewServer();
  browser = await puppeteer.launch({ headless: 'new', args: ['--no-sandbox'] });

  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 900 });
  await page.goto(BASE_URL, { waitUntil: 'networkidle0', timeout: 20000 });

  // Scroll to the Animated Navigation section to ensure it is rendered
  await page.evaluate(() => {
    const el = Array.from(document.querySelectorAll('h2')).find(h =>
      h.textContent.includes('Animated Navigation'),
    );
    el?.scrollIntoView({ block: 'center' });
  });
  await sleep(400);

  // ---------------------------------------------------------------------------
  // 1. Expanded state (default)
  // ---------------------------------------------------------------------------
  const expandedMetrics = await page.evaluate((navItemH) => {
    const pill = document.querySelector('.aef-nav-glidepill');
    if (!pill) return { error: '.aef-nav-glidepill not found' };
    const rect  = pill.getBoundingClientRect();
    const style = window.getComputedStyle(pill);
    return {
      width:        rect.width,
      height:       rect.height,
      borderRadius: style.borderTopLeftRadius,
      rawBorderRadius: style.borderRadius,
    };
  }, NAV_ITEM_H);

  if (expandedMetrics.error) throw new Error(expandedMetrics.error);

  // Width must be > NAV_ITEM_H (pill spans the row)
  if (expandedMetrics.width <= NAV_ITEM_H) {
    throw new Error(
      `Expanded pill width ${expandedMetrics.width}px should be > NAV_ITEM_H (${NAV_ITEM_H}px)`,
    );
  }

  // border-radius must not be 50% (which for 34px square = 17px)
  const halfH = NAV_ITEM_H / 2;
  const brPx  = parseFloat(expandedMetrics.borderRadius);
  if (Math.abs(brPx - halfH) < 2) {
    throw new Error(
      `Expanded pill border-radius ${expandedMetrics.borderRadius} looks circular (expected pill, not circle)`,
    );
  }

  console.log(`[PASS] Expanded: width=${expandedMetrics.width}px, border-radius=${expandedMetrics.borderRadius}`);

  // ---------------------------------------------------------------------------
  // 2. Click Collapse, verify collapsed state
  // ---------------------------------------------------------------------------
  await page.evaluate(() => {
    const btn = Array.from(document.querySelectorAll('button')).find(b =>
      b.textContent.trim().startsWith('Collapse'),
    );
    btn?.click();
  });
  await sleep(500); // wait for CSS transition to settle

  const collapsedMetrics = await page.evaluate((navItemH) => {
    const pill = document.querySelector('.aef-nav-glidepill');
    if (!pill) return { error: '.aef-nav-glidepill not found' };
    const rect  = pill.getBoundingClientRect();
    const style = window.getComputedStyle(pill);
    return {
      width:        rect.width,
      height:       rect.height,
      borderRadius: style.borderTopLeftRadius,
    };
  }, NAV_ITEM_H);

  if (collapsedMetrics.error) throw new Error(collapsedMetrics.error);

  // Width must be approximately NAV_ITEM_H (circle behind icon)
  if (Math.abs(collapsedMetrics.width - NAV_ITEM_H) > 4) {
    throw new Error(
      `Collapsed pill width ${collapsedMetrics.width}px should be ~${NAV_ITEM_H}px`,
    );
  }

  // border-radius must be ~50% of height (circular).
  // getComputedStyle may return % (inline value) or px (resolved value); handle both.
  const rawBr = collapsedMetrics.borderRadius;
  let collapsedBrPx;
  if (rawBr.endsWith('%')) {
    collapsedBrPx = (parseFloat(rawBr) / 100) * collapsedMetrics.height;
  } else {
    collapsedBrPx = parseFloat(rawBr);
  }
  const expectedCircleR = collapsedMetrics.height / 2;
  if (Math.abs(collapsedBrPx - expectedCircleR) > 3) {
    throw new Error(
      `Collapsed pill border-radius ${collapsedMetrics.borderRadius} is not circular (expected ~${expectedCircleR}px, got ~${collapsedBrPx}px)`,
    );
  }

  console.log(`[PASS] Collapsed: width=${collapsedMetrics.width}px, border-radius=${collapsedMetrics.borderRadius}`);

  // ---------------------------------------------------------------------------
  // 3. No horizontal overflow at 390px (mobile) and 1440px (desktop)
  // ---------------------------------------------------------------------------
  for (const vw of [390, 1440]) {
    await page.setViewport({ width: vw, height: 900 });
    await sleep(200);
    const overflow = await page.evaluate(() => {
      return document.documentElement.scrollWidth > document.documentElement.clientWidth;
    });
    if (overflow) {
      throw new Error(`Horizontal overflow detected at viewport width ${vw}px`);
    }
    console.log(`[PASS] No horizontal overflow at ${vw}px`);
  }

  console.log('\nPARYTY-NAV-SELECTOR-001 dom geometry checks: ALL PASS');
} catch (err) {
  console.error('\nPARYTY-NAV-SELECTOR-001 FAILED:', err.message);
  failed = true;
} finally {
  if (browser) await browser.close();
  if (server)  server.kill();
}

if (failed) process.exit(1);
