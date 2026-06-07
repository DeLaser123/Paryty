/**
 * ds_nav_foreground_color.test.mjs
 *
 * Puppeteer proof: animated nav active-item foreground color.
 *
 * Expanded mode  : active label text resolves to black (rgb(0, 0, 0) / --aef-selected-text)
 * Expanded mode  : active icon resolves to black (rgb(0, 0, 0) / --aef-selected-icon)
 * Collapsed mode : active icon resolves to black (rgb(0, 0, 0) / --aef-selected-icon)
 * Selector shape : pill (expanded) and circle (collapsed) geometry unchanged
 * Overflow       : no horizontal overflow at 390 and 1440 px viewport widths
 *
 * Runs against `vite preview` serving ui/dist (build must be current).
 * Launched by: node ui/src/paryty_design_system/__tests__/ds_nav_foreground_color.test.mjs
 */

import puppeteer from 'puppeteer';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here   = dirname(fileURLToPath(import.meta.url));
const uiRoot = resolve(here, '../../../');

const PREVIEW_PORT = 4180;
const BASE_URL     = `http://localhost:${PREVIEW_PORT}`;
const NAV_ITEM_H   = 34; // must match AnimatedNavSection constant

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------
function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

async function startPreviewServer() {
  const viteBin = resolve(uiRoot, 'node_modules/vite/bin/vite.js');
  const srv = spawn(
    process.execPath,
    [viteBin, 'preview', '--port', String(PREVIEW_PORT), '--strictPort'],
    { cwd: uiRoot, stdio: 'ignore', shell: false, detached: false },
  );
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
    setTimeout(poll, 800);
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

  // Scroll Animated Navigation into view
  await page.evaluate(() => {
    const el = Array.from(document.querySelectorAll('h2')).find(h =>
      h.textContent.includes('Animated Navigation'),
    );
    el?.scrollIntoView({ block: 'center' });
  });
  await sleep(400);

  // ---------------------------------------------------------------------------
  // 1. Expanded: active label text must be black
  // ---------------------------------------------------------------------------
  const expandedTextColor = await page.evaluate(() => {
    const activeItem = document.querySelector('.aef-anim-nav-item--active');
    if (!activeItem) return { error: '.aef-anim-nav-item--active not found' };
    const label = activeItem.querySelector('.aef-anim-nav-label');
    if (!label) return { error: '.aef-anim-nav-label not found inside active item' };
    return { color: window.getComputedStyle(label).color };
  });

  if (expandedTextColor.error) throw new Error(expandedTextColor.error);
  if (expandedTextColor.color !== 'rgb(0, 0, 0)') {
    throw new Error(
      `Expanded active label color should be rgb(0, 0, 0) but got "${expandedTextColor.color}"`,
    );
  }
  console.log(`[PASS] Expanded active label color: ${expandedTextColor.color}`);

  // ---------------------------------------------------------------------------
  // 1b. Expanded: active icon color must be black (--aef-selected-icon)
  // ---------------------------------------------------------------------------
  const expandedIconColor = await page.evaluate(() => {
    const activeItem = document.querySelector('.aef-anim-nav-item--active');
    if (!activeItem) return { error: '.aef-anim-nav-item--active not found' };
    const icon = activeItem.querySelector('.aef-anim-nav-icon');
    if (!icon) return { error: '.aef-anim-nav-icon not found inside active item (expanded)' };
    return { color: window.getComputedStyle(icon).color };
  });

  if (expandedIconColor.error) throw new Error(expandedIconColor.error);
  if (expandedIconColor.color !== 'rgb(0, 0, 0)') {
    throw new Error(
      `Expanded active icon color should be rgb(0, 0, 0) but got "${expandedIconColor.color}"`,
    );
  }
  console.log(`[PASS] Expanded active icon color: ${expandedIconColor.color}`);

  // ---------------------------------------------------------------------------
  // 2. Expanded: pill selector remains pill (not circle)
  // ---------------------------------------------------------------------------
  const pillShape = await page.evaluate((navItemH) => {
    const pill = document.querySelector('.aef-nav-glidepill');
    if (!pill) return { error: '.aef-nav-glidepill not found' };
    const rect = pill.getBoundingClientRect();
    const br   = parseFloat(window.getComputedStyle(pill).borderTopLeftRadius);
    return { width: rect.width, height: rect.height, brPx: br };
  }, NAV_ITEM_H);

  if (pillShape.error) throw new Error(pillShape.error);
  if (pillShape.width <= NAV_ITEM_H) {
    throw new Error(`Expanded pill width ${pillShape.width}px must be > ${NAV_ITEM_H}px`);
  }
  // border-radius must NOT be exactly circular (17px for h=34)
  if (Math.abs(pillShape.brPx - NAV_ITEM_H / 2) < 2) {
    throw new Error(`Expanded pill border-radius ${pillShape.brPx}px looks circular; expected pill`);
  }
  console.log(`[PASS] Expanded pill shape: width=${pillShape.width}px, border-radius=${pillShape.brPx}px`);

  // ---------------------------------------------------------------------------
  // 3. Collapse, then check collapsed active icon color
  // ---------------------------------------------------------------------------
  await page.evaluate(() => {
    const btn = Array.from(document.querySelectorAll('button')).find(b =>
      b.textContent.trim().startsWith('Collapse'),
    );
    btn?.click();
  });
  await sleep(500);

  const collapsedIconColor = await page.evaluate(() => {
    const activeItem = document.querySelector('.aef-anim-nav-item--active');
    if (!activeItem) return { error: '.aef-anim-nav-item--active not found' };
    const icon = activeItem.querySelector('.aef-anim-nav-icon');
    if (!icon) return { error: '.aef-anim-nav-icon not found inside active item' };
    return { color: window.getComputedStyle(icon).color };
  });

  if (collapsedIconColor.error) throw new Error(collapsedIconColor.error);
  if (collapsedIconColor.color !== 'rgb(0, 0, 0)') {
    throw new Error(
      `Collapsed active icon color should be rgb(0, 0, 0) but got "${collapsedIconColor.color}"`,
    );
  }
  console.log(`[PASS] Collapsed active icon color: ${collapsedIconColor.color}`);

  // ---------------------------------------------------------------------------
  // 4. Collapsed: selector remains circular
  // ---------------------------------------------------------------------------
  const circleShape = await page.evaluate((navItemH) => {
    const pill = document.querySelector('.aef-nav-glidepill');
    if (!pill) return { error: '.aef-nav-glidepill not found' };
    const rect  = pill.getBoundingClientRect();
    const rawBr = window.getComputedStyle(pill).borderTopLeftRadius;
    let brPx = rawBr.endsWith('%')
      ? (parseFloat(rawBr) / 100) * rect.height
      : parseFloat(rawBr);
    return { width: rect.width, height: rect.height, brPx };
  }, NAV_ITEM_H);

  if (circleShape.error) throw new Error(circleShape.error);
  if (Math.abs(circleShape.width - NAV_ITEM_H) > 4) {
    throw new Error(`Collapsed circle width ${circleShape.width}px should be ~${NAV_ITEM_H}px`);
  }
  const expectedR = circleShape.height / 2;
  if (Math.abs(circleShape.brPx - expectedR) > 3) {
    throw new Error(
      `Collapsed circle border-radius ${circleShape.brPx}px should be ~${expectedR}px`,
    );
  }
  console.log(`[PASS] Collapsed circle shape: width=${circleShape.width}px, border-radius=${circleShape.brPx}px (= height/2, not height)`);

  // ---------------------------------------------------------------------------
  // 5. No horizontal overflow at 390 and 1440
  // ---------------------------------------------------------------------------
  for (const vw of [390, 1440]) {
    await page.setViewport({ width: vw, height: 900 });
    await sleep(200);
    const overflow = await page.evaluate(() =>
      document.documentElement.scrollWidth > document.documentElement.clientWidth,
    );
    if (overflow) throw new Error(`Horizontal overflow at ${vw}px`);
    console.log(`[PASS] No horizontal overflow at ${vw}px`);
  }

  console.log('\nPARYTY-DS-NAV-TEXT-001 foreground color checks: ALL PASS');
} catch (err) {
  console.error('\nPARYTY-DS-NAV-TEXT-001 FAILED:', err.message);
  failed = true;
} finally {
  if (browser) await browser.close();
  if (server)  server.kill();
}

if (failed) process.exit(1);
