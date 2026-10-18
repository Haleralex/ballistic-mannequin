// Opens the simulator in headless Chromium against the files in this repository.
// The CDN imports (three, rapier) are answered from tests/node_modules, so the tests run offline and pin the same
// versions as the import map. WebGL goes through SwiftShader: slow, but the same on every machine.
//
// Chromium: CHROMIUM_PATH, or the browser that `npx playwright-core install chromium` downloaded.
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const ROOT = path.resolve(HERE, '../..');
const NM = path.resolve(HERE, '../node_modules');
const TYPES = { '.html': 'text/html', '.js': 'application/javascript', '.mjs': 'application/javascript', '.json': 'application/json', '.css': 'text/css', '.png': 'image/png', '.svg': 'image/svg+xml' };
export const ORIGIN = 'http://app.test';

export async function launch() {
  const executablePath = process.env.CHROMIUM_PATH || undefined;
  return chromium.launch({ executablePath, args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader'] });
}

// a page with the simulator loaded and the ?debug hook ready; page.errors collects uncaught page errors
export async function openSim(browser, { query = 'debug', viewport = { width: 1300, height: 800 }, route } = {}) {
  const page = await browser.newPage({ viewport });
  page.errors = [];
  page.on('pageerror', e => page.errors.push(e.message));
  await page.route('**/*', async r => {
    const url = new URL(r.request().url());
    if (route && await route(r, url)) return;
    if (url.origin === ORIGIN) {
      const file = path.join(ROOT, url.pathname === '/' ? 'index.html' : decodeURIComponent(url.pathname));
      if (!file.startsWith(ROOT) || !fs.existsSync(file)) return r.fulfill({ status: 404, body: 'not found' });
      return r.fulfill({ body: fs.readFileSync(file), contentType: TYPES[path.extname(file)] || 'application/octet-stream' });
    }
    const m = url.href.match(/cdn\.jsdelivr\.net\/npm\/((?:@[^/]+\/)?[^@/]+)@[^/]+\/(.+)$/);
    if (m) return r.fulfill({ body: fs.readFileSync(path.join(NM, m[1], m[2])), contentType: 'application/javascript' });
    return r.abort(); // fonts and anything else external: not needed for the simulation
  });
  await page.goto(`${ORIGIN}/?${query}`);
  await page.waitForFunction(() => window.__sim, null, { timeout: 60000 });
  return page;
}

// advance the simulation by whole physics steps with the real-time loop paused
export function steps(page, n) {
  return page.evaluate(n => { const s = window.__sim; s.setTime('t0'); for (let i = 0; i < n; i++) s.step(); }, n);
}

// shoot at a body part (offset in metres in screen-aligned world x/y) through the camera, as a click would
export function shootAt(page, part, dx = 0, dy = 0) {
  return page.evaluate(([part, dx, dy]) => {
    const s = window.__sim, p = s.byName()[part].mesh.position.clone();
    p.x += dx; p.y += dy;
    s.camera.updateMatrixWorld(); p.project(s.camera);
    const c = document.getElementById('c').getBoundingClientRect();
    s.shoot(c.left + (p.x + 1) / 2 * c.width, c.top + (1 - p.y) / 2 * c.height);
  }, [part, dx, dy]);
}
