// Growth features (team C): offline behaviour, the clip, and the whole online path against the real Go server with
// a fresh Postgres database: visit → variants → shot → events in the database → share → /r/<id> → replay → take
// over → own shot → share_convert → rating → waitlist.
//
// The online tests build server/ with Go and need a Postgres 16 that accepts the postgres user without a password
// (MQ_PG_HOST, default the socket dir /tmp/pg_mq; MQ_PG_PORT, default 55432). Without them they are skipped.
// GROWTH_SCREENS=<dir> also saves screenshots (desktop 1300×800, phone 390×844) of the new UI for a visual check.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import net from 'node:net';
import path from 'node:path';
import { launch, openSim, steps, ROOT } from './lib/harness.mjs';

const NM = path.join(ROOT, 'tests/node_modules');
const PG_HOST = process.env.MQ_PG_HOST || '/tmp/pg_mq', PG_PORT = process.env.MQ_PG_PORT || '55432';
const SCREENS = process.env.GROWTH_SCREENS ? path.resolve(process.env.GROWTH_SCREENS) : null;
const DESKTOP = { width: 1300, height: 800 }, PHONE = { width: 390, height: 844 };
const wait = ms => new Promise(r => setTimeout(r, ms));

function have(cmd, args) { try { execFileSync(cmd, args, { stdio: 'ignore' }); return true; } catch { return false; } }
const serverSkip = !have('go', ['version']) ? 'go is not installed'
  : !have('pg_isready', ['-h', PG_HOST, '-p', PG_PORT]) ? `no Postgres at ${PG_HOST}:${PG_PORT}` : false;

let browser;
before(async () => { browser = await launch(); });
after(async () => { await browser?.close(); });

// ---------- offline: the page as GitHub Pages serves it, without the API ----------

test('offline: the simulator works, online features hide, the clip stays', async () => {
  const page = await openSim(browser);
  assert.equal(await page.evaluate(() => window.__growth.online), false, '/healthz does not answer: offline');
  assert.equal(await page.evaluate(() => document.documentElement.classList.contains('online')), false);
  for (const id of ['shareBtn', 'doorRoom', 'doorPro']) assert.equal(await page.isVisible('#' + id), false, `#${id} hidden offline`);
  assert.equal(await page.isVisible('#intro'), true, 'a first visit shows the intro');
  assert.equal(await page.textContent('#introTitle'), 'Как тело реагирует на ранение', 'offline the intro is variant a');
  await page.click('#introCta');
  assert.equal(await page.isVisible('#intro'), false);
  assert.equal(await page.isVisible('#clipBtn'), true, 'the clip works without the server');
  // the simulator still shoots and nothing is sent anywhere
  const sent = [];
  page.on('request', r => { if (r.url().includes('/api/')) sent.push(r.url()); });
  await page.evaluate(() => { const s = window.__sim, t = s.byName().chest.body.translation(), o = [0.9, 1.45, 4.2];
    const d = [t.x - o[0], t.y - o[1], t.z - o[2]], l = Math.hypot(...d); s.fire(o, d.map(v => v / l)); });
  await steps(page, 120);
  assert.equal(await page.evaluate(() => window.__sim.character().wounds.length > 0), true);
  await page.evaluate(() => window.__growth.flush());
  await wait(200);
  assert.deepEqual(sent, [], 'no API requests offline');
  // a returning visitor does not see the intro again
  await page.reload();
  await page.waitForFunction(() => window.__sim, null, { timeout: 60000 });
  assert.equal(await page.isVisible('#intro'), false, 'no intro on a return visit');
  assert.deepEqual(page.errors, []);
  await page.close();
});

test('offline: the "без крови" switch hides blood and is remembered', async () => {
  const page = await openSim(browser);
  await page.click('#introNoBlood');
  assert.equal(await page.evaluate(() => document.getElementById('showBlood').checked), false, 'sim.setBlood(false)');
  await page.click('#introCta');
  assert.equal(await page.isChecked('#noBlood'), true, 'the panel switch follows the intro one');
  await page.reload();
  await page.waitForFunction(() => window.__sim, null, { timeout: 60000 });
  await page.waitForFunction(() => document.getElementById('noBlood').checked);
  assert.equal(await page.evaluate(() => document.getElementById('showBlood').checked), false, 'remembered after a reload');
  await page.click('#noBlood');
  assert.equal(await page.evaluate(() => document.getElementById('showBlood').checked), true);
  assert.deepEqual(page.errors, []);
  await page.close();
});

test('offline: a ?case= link explains itself instead of failing', async () => {
  const page = await openSim(browser, { query: 'debug&case=a7F3kQ9x' });
  await page.waitForSelector('#toast:not([hidden])');
  assert.match(await page.textContent('#toast'), /только на сайте/);
  assert.equal(await page.isVisible('#intro'), false, 'no intro for a shared link');
  assert.deepEqual(page.errors, []);
  await page.close();
});

test('offline, phone: no horizontal scroll, intro fits', async () => {
  const page = await openSim(browser, { viewport: PHONE });
  const box = await page.locator('.intro-card').boundingBox();
  assert.ok(box.x >= 0 && box.x + box.width <= PHONE.width, 'the intro card fits the width');
  await page.click('#introCta');
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'no horizontal scroll');
  assert.deepEqual(page.errors, []);
  await page.close();
});

// ---------- the clip ----------

test('clip: frames are composed with the watermark and recorded into a downloadable file', async () => {
  const page = await openSim(browser);
  await page.click('#introCta');
  // a composed frame copied right after the simulator's render is a picture, not an empty buffer
  const frame = await page.evaluate(async () => {
    const c = await window.__growth.clip.snapshot(), ctx = c.getContext('2d');
    const d = ctx.getImageData(0, 0, c.width, Math.floor(c.height * 0.8)).data; // the scene, above the watermark
    let lo = 255, hi = 0, opaque = 0;
    for (let i = 0; i < d.length; i += 4 * 97) { const v = d[i] + d[i + 1] + d[i + 2]; lo = Math.min(lo, v / 3); hi = Math.max(hi, v / 3); if (d[i + 3] === 255) opaque++; }
    const mark = ctx.getImageData(c.width - 60, c.height - 30, 40, 16).data; // the watermark's corner
    let bright = 0;
    for (let i = 0; i < mark.length; i += 4) if (mark[i] > 200 && mark[i + 1] > 200) bright++;
    return { w: c.width, h: c.height, spread: hi - lo, opaque, bright };
  });
  assert.ok(frame.w > 0 && frame.w <= 1280 && frame.h <= 720 && frame.w % 2 === 0 && frame.h % 2 === 0, `clip size ${frame.w}×${frame.h}`);
  assert.ok(frame.opaque > 0 && frame.spread > 20, `the scene is in the frame (brightness spread ${frame.spread})`);
  assert.ok(frame.bright > 0, 'the watermark is drawn in the corner');

  const download = page.waitForEvent('download', { timeout: 30000 });
  await page.click('#clipBtn');
  await page.waitForFunction(() => document.getElementById('clipBtn').getAttribute('aria-pressed') === 'true');
  await wait(2500);
  await page.click('#clipBtn');
  const dl = await download;
  const file = await dl.path();
  const size = fs.statSync(file).size;
  assert.ok(size > 1000, `the clip has content (${size} bytes)`);
  assert.match(dl.suggestedFilename(), /^manekin-.*\.(mp4|webm)$/);
  const last = await page.evaluate(() => window.__growth.clip.last);
  assert.ok(last.seconds >= 2 && last.seconds <= 20, `recorded ${last.seconds} s`);
  assert.match(await page.textContent('#shareNote'), /Клип .* сохранён/);
  assert.deepEqual(page.errors, []);
  await page.close();
});

// ---------- online: the real server and a fresh database ----------

let srv = null; // { base, proc, db, psql }

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.once('error', reject);
    s.listen(0, '127.0.0.1', () => { const { port } = s.address(); s.close(() => resolve(port)); });
  });
}

async function startServer() {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'mq-growth-'));
  const bin = path.join(tmp, 'mq-api');
  execFileSync('go', ['build', '-o', bin, './cmd/api'], { cwd: path.join(ROOT, 'server'), stdio: 'inherit' });
  const db = `mq_growth_${process.pid}_${Date.now()}`;
  const pg = ['-h', PG_HOST, '-p', PG_PORT, '-U', 'postgres'];
  execFileSync('createdb', [...pg, db]);
  const port = await freePort();
  const host = PG_HOST.startsWith('/') ? `host=${encodeURIComponent(PG_HOST)}` : `host=${PG_HOST}`;
  const proc = spawn(bin, [], {
    env: { ...process.env, DATABASE_URL: `postgres://postgres@/${db}?${host}&port=${PG_PORT}&sslmode=disable`, STATIC_DIR: ROOT,
      PORT: String(port), BUILD: 'growthtest', ADMIN_USER: 'admin', ADMIN_PASSWORD: 'admin-test' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let log = '';
  proc.stdout.on('data', d => { log += d; });
  proc.stderr.on('data', d => { log += d; });
  const base = `http://127.0.0.1:${port}`;
  for (let i = 0; ; i++) {
    try { const r = await fetch(base + '/healthz'); if (r.ok) break; } catch { /* not listening yet */ }
    if (i > 300 || proc.exitCode !== null) throw new Error('the server did not start:\n' + log);
    await wait(100);
  }
  // a query as JSON rows
  const psql = sql => JSON.parse(execFileSync('psql', [...pg, '-d', db, '-At', '-c', `select coalesce(json_agg(t), '[]') from (${sql}) t`]).toString());
  return { base, proc, db, pg, tmp, psql, log: () => log };
}

async function stopServer() {
  if (!srv) return;
  srv.proc.kill('SIGTERM');
  await new Promise(r => { if (srv.proc.exitCode !== null) r(); else srv.proc.once('exit', r); setTimeout(r, 20000); });
  try { execFileSync('dropdb', [...srv.pg, '--if-exists', srv.db]); } catch { /* left behind: harmless */ }
  fs.rmSync(srv.tmp, { recursive: true, force: true });
  srv = null;
}
after(stopServer);

// a page on the server; the CDN imports come from tests/node_modules as in the harness, fonts are not needed
async function openOnline(url, { viewport = DESKTOP, debug = true } = {}) {
  const page = await browser.newPage({ viewport });
  page.errors = [];
  page.on('pageerror', e => page.errors.push(e.message));
  await page.route('**/*', r => {
    const u = new URL(r.request().url());
    if (u.origin === srv.base) return r.continue();
    const m = u.href.match(/cdn\.jsdelivr\.net\/npm\/((?:@[^/]+\/)?[^@/]+)@[^/]+\/(.+)$/);
    if (m) return r.fulfill({ body: fs.readFileSync(path.join(NM, m[1], m[2])), contentType: 'application/javascript' });
    return r.abort();
  });
  await page.goto(url);
  if (debug) await page.waitForFunction(() => window.__sim, null, { timeout: 60000 });
  return page;
}
const vidOf = page => page.evaluate(() => localStorage.getItem('mq_vid'));
const eventsOf = (vid, extra = '') => srv.psql(`select name, props from events where visitor_id = '${vid}' ${extra} order by id`);
async function until(fn, what, ms = 15000) {
  const t0 = Date.now();
  for (;;) {
    const v = await fn();
    if (v) return v;
    if (Date.now() - t0 > ms) throw new Error('timed out waiting for ' + what);
    await wait(150);
  }
}
const fireAtChest = page => page.evaluate(() => { const s = window.__sim, t = s.byName().chest.body.translation(), o = [0.9, 1.45, 4.2];
  const d = [t.x - o[0], t.y - o[1], t.z - o[2]], l = Math.hypot(...d); s.fire(o, d.map(v => v / l)); });

// a shot of the viewer's own whose scenario then runs 10 s of simulated time (reaction-watched); a scenario can be
// replaced before that (a fall starts another one), so step until it is watched, shooting again if need be.
// Returns the number of shots fired.
async function watchOwnReaction(page) {
  await page.evaluate(async () => { const bus = await import('/bus.js'); window.__watched = 0; bus.on('reaction-watched', () => { window.__watched++; }); });
  for (let shots = 1; shots <= 4; shots++) {
    await fireAtChest(page);
    for (let k = 0; k < 40; k++) {
      await steps(page, 120);
      if (await page.evaluate(() => window.__watched)) return shots;
    }
  }
  throw new Error('no scenario ran for 10 s');
}

let shared = null; // { id, vid } from the first online test

test('online: visit → variants → shot → events in Postgres → rating → share', { skip: serverSkip }, async () => {
  srv = await startServer();
  const page = await openOnline(`${srv.base}/?debug`);
  await page.waitForFunction(() => document.documentElement.classList.contains('online'));
  for (const id of ['shareBtn', 'doorRoom', 'doorPro']) assert.equal(await page.isVisible('#' + id), true, `#${id} shown online`);
  const vid = await vidOf(page);
  assert.match(vid, /^[0-9a-f-]{36}$/);
  await page.waitForFunction(() => localStorage.getItem('mq_exp'));
  const assigned = Object.fromEntries(srv.psql(`select experiment_key, variant from assignments where visitor_id = '${vid}'`).map(r => [r.experiment_key, r.variant]));
  assert.deepEqual(Object.keys(assigned).sort(), ['t1_positioning', 't2_onboarding'], 'the active experiments are assigned');
  assert.deepEqual(JSON.parse(await page.evaluate(() => localStorage.getItem('mq_exp'))), assigned, 'cached in the browser');

  // the intro shows the visitor's T1 variant (or a if the answer came after 600 ms)
  await page.waitForFunction(() => !document.getElementById('introCopy').classList.contains('pending'));
  const shown = await page.evaluate(() => document.getElementById('intro').dataset.variant);
  assert.ok(shown === assigned.t1_positioning || shown === 'a', `intro variant ${shown}, assigned ${assigned.t1_positioning}`);
  await page.click('#introCta');

  // a shot of the viewer's own, and its scenario watched for 10 s of simulated time
  const fired = await watchOwnReaction(page);
  // the rating comes about 5 s later
  await page.waitForSelector('#rate:not([hidden])', { timeout: 20000 });
  await page.click('#rate [data-v="1"]');
  await page.waitForSelector('#rateForm:not([hidden])');
  await page.fill('#rateComment', 'рука к ране — правдоподобно');
  await page.click('#rateForm button[type=submit]');
  await page.waitForFunction(() => /Спасибо, прочитаем/.test(document.getElementById('rateMsg').textContent));
  const ratings = srv.psql(`select scenario_id, value, comment from ratings where visitor_id = '${vid}'`);
  assert.equal(ratings.length, 1, 'one rating, the comment updated it');
  assert.equal(ratings[0].value, 1);
  assert.equal(ratings[0].comment, 'рука к ране — правдоподобно');
  assert.ok(ratings[0].scenario_id.length > 0);

  // share: the case is stored and the link shown
  await page.click('#shareBtn');
  await page.waitForFunction(() => /\/r\/[0-9A-Za-z]{8}$/.test(document.getElementById('shareUrl').value));
  const url = await page.inputValue('#shareUrl');
  assert.ok(url.startsWith(srv.base + '/r/'), url);
  const id = url.split('/r/')[1];
  const cases = srv.psql(`select id, kind, visitor_id from cases where id = '${id}'`);
  assert.equal(cases.length, 1, 'the case is in the database');
  assert.equal(cases[0].visitor_id, vid);

  await page.evaluate(() => window.__growth.flush());
  const names = await until(() => { const n = eventsOf(vid).map(e => e.name); return n.includes('share_link') && n; }, 'events in Postgres');
  for (const n of ['visit', 'landing_cta', 'sim_ready', 'shot', 'reaction_watched', 'rate', 'share_link']) assert.ok(names.includes(n), `${n} stored (${names})`);
  const ev = Object.fromEntries(eventsOf(vid).map(e => [e.name, e.props]));
  assert.equal(ev.visit.page, 'landing');
  assert.equal(ev.landing_cta.variant, shown);
  assert.equal(ev.shot.hit, true);
  assert.equal(ev.shot.weapon, 'pistol');
  assert.equal(ev.share_link.case_id, id);
  assert.equal(typeof ev.sim_ready.load_ms, 'number');
  assert.equal(names.filter(n => n === 'shot').length, fired, 'every shot, once');
  assert.deepEqual(page.errors, []);
  shared = { id, vid, page };
});

test('online: /r/<id> → replay → seek, pause → take over → own shot → share_convert', { skip: serverSkip }, async () => {
  assert.ok(shared, 'a case was shared by the previous test');
  const page = await openOnline(`${srv.base}/r/${shared.id}`, { debug: false });
  await page.waitForURL(u => u.search === `?case=${shared.id}`, { timeout: 15000 });
  await page.waitForSelector('#rbar:not([hidden])', { timeout: 60000 });
  assert.equal(await page.isVisible('#intro'), false, 'no intro for a shared link');
  const sim = () => page.evaluate(async () => { const { sim } = await import('/bus.js'); return { step: sim.step, replaying: sim.replaying }; });
  const a = await sim();
  assert.equal(a.replaying, true, 'the case plays back');
  await until(async () => (await sim()).step > a.step + 30, 'the replay to advance in real time');
  // shots of the replay are not the viewer's
  const vid = await vidOf(page);
  // seek through the bar
  await page.evaluate(() => { const r = document.getElementById('rbarSeek'); r.value = 900; r.dispatchEvent(new Event('input')); r.dispatchEvent(new Event('change')); });
  await until(async () => (await sim()).step >= 900, 'the seek');
  assert.match(await page.textContent('#rbarTime'), /^\d+,\d \/ \d+,\d с$/);
  // pause and play
  await page.click('#rbarPlay');
  assert.equal(await page.getAttribute('#t0', 'aria-pressed'), 'true', 'paused');
  await page.click('#rbarPlay');
  assert.equal(await page.getAttribute('#t1', 'aria-pressed'), 'true', 'playing again');
  // a click on the scene during the replay does not shoot, it points at the button
  await page.click('#c');
  await page.waitForSelector('#toast:not([hidden])');
  assert.match(await page.textContent('#toast'), /Сделать свой выстрел/);

  await page.click('#rbarTake');
  assert.equal((await sim()).replaying, false, 'taken over');
  assert.equal(await page.isVisible('#rbar'), false, 'the bar is gone');
  await page.click('#c'); // the viewer's own shot, through the camera at the middle of the scene
  await page.evaluate(() => dispatchEvent(new Event('pagehide'))); // sendBeacon
  const evs = await until(() => { const e = eventsOf(vid); return e.some(x => x.name === 'share_convert') && e; }, 'share_convert in Postgres');
  const by = n => evs.filter(e => e.name === n);
  assert.equal(by('visit')[0].props.page, 'replay');
  assert.equal(by('share_open').length, 1);
  assert.equal(by('share_open')[0].props.case_id, shared.id);
  assert.equal(by('share_convert').length, 1);
  assert.equal(by('share_convert')[0].props.case_id, shared.id);
  assert.equal(by('shot').length, 1, 'only the own shot counts, not the replayed one');
  const conv = srv.psql(`select * from share_conversion where case_id = '${shared.id}'`);
  assert.equal(conv.length, 1, 'the share_conversion view sees it');
  assert.equal(conv[0].converted, 1);
  const views = srv.psql(`select views from cases where id = '${shared.id}'`)[0].views;
  assert.ok(views >= 1, 'the view was counted');
  assert.deepEqual(page.errors, []);
  if (SCREENS) shared.replayPage = page; else await page.close();
});

test('online: fake doors and the waitlist (a repeated address is still accepted)', { skip: serverSkip }, async () => {
  const page = shared?.page || await openOnline(`${srv.base}/?debug`);
  if (await page.isVisible('#intro')) await page.click('#introCta');
  const vid = await vidOf(page);

  await page.click('#doorRoom');
  assert.equal(await page.evaluate(() => document.getElementById('roomDlg').open), true, 'the room modal opens');
  assert.equal(await page.evaluate(() => document.activeElement.id), 'roomEmail', 'focus moves into the modal');
  await page.keyboard.press('Escape');
  assert.equal(await page.evaluate(() => document.getElementById('roomDlg').open), false, 'Escape closes it');
  assert.equal(await page.evaluate(() => document.activeElement.id), 'doorRoom', 'focus returns to the button');

  const join = async (door, email, { consent = true } = {}) => {
    const dlg = door === 'room' ? '#roomDlg' : '#proDlg', pre = door === 'room' ? 'room' : 'pro';
    await page.click(door === 'room' ? '#doorRoom' : '#doorPro');
    if (door === 'pro') {
      assert.equal(await page.textContent('#proPrice'), '299 ₽/мес', 't4_price is inactive: variant a');
      await page.click('#proBuy');
    }
    await page.fill(`#${pre}Email`, email);
    if (consent) await page.check(`#${pre}Consent`);
    await page.click(`${dlg} form.wl:not([hidden]) button[type=submit]`);
  };
  // without consent nothing is sent
  await join('room', 'actor@example.ru', { consent: false });
  assert.match(await page.textContent('#roomDlg .status'), /согласия/);
  await page.check('#roomConsent');
  await page.click('#roomDlg button[type=submit]');
  await page.waitForSelector('#roomDlg .wl-done:not([hidden])');
  await page.click('#roomDlg .wl-done button');
  await join('room', 'Actor@Example.ru');
  await page.waitForSelector('#roomDlg .wl-done:not([hidden])');
  await page.click('#roomDlg .wl-done button');
  await join('pro', 'director@example.ru');
  await page.waitForSelector('#proWait .wl-done:not([hidden])');
  await page.keyboard.press('Escape');

  const rows = srv.psql(`select email, source, price_variant from waitlist order by email`);
  assert.deepEqual(rows, [{ email: 'actor@example.ru', source: 'room', price_variant: null }, { email: 'director@example.ru', source: 'pro', price_variant: 'a' }]);
  await page.evaluate(() => window.__growth.flush());
  const evs = await until(() => { const e = eventsOf(vid, `and name in ('fake_door_open', 'fake_door_click', 'waitlist_submit')`); return e.length >= 7 && e; }, 'door events');
  const count = (n, door) => evs.filter(e => e.name === n && e.props.door === door).length;
  assert.equal(count('fake_door_open', 'room'), 3);
  assert.equal(count('fake_door_open', 'pro'), 1);
  assert.equal(count('fake_door_click', 'pro'), 1);
  assert.equal(evs.find(e => e.name === 'fake_door_click').props.price_variant, 'a');
  assert.equal(count('waitlist_submit', 'room'), 2, 'the repeated address is accepted too');
  assert.equal(count('waitlist_submit', 'pro'), 1);
  assert.deepEqual(page.errors, []);
  if (!SCREENS) await page.close();
});

test('online: T2 variant b points at the chest and explains the first shot', { skip: serverSkip }, async () => {
  const page = await openOnline(`${srv.base}/?debug&exp=t2_onboarding:b`);
  await page.click('#introCta');
  await page.waitForSelector('#onbMarker:not([hidden])');
  assert.match(await page.textContent('#onbTip'), /Кликните по груди манекена/);
  // the marker sits on the chest: a click there hits it
  const m = await page.locator('#onbMarker').boundingBox();
  await page.mouse.click(m.x + m.width / 2, m.y + m.height / 2);
  await page.waitForFunction(() => /реакцию задаёт задетый орган/.test(document.getElementById('onbTip').textContent));
  const wounds = await page.evaluate(() => window.__sim.character().wounds.map(w => w.zone));
  assert.ok(wounds.some(z => /груд/.test(z)), `the marked point is the chest (${wounds})`);
  assert.equal(await page.isVisible('#onbMarker'), false);
  assert.deepEqual(page.errors, []);
  if (SCREENS) await shot(page, 'onboarding-after-shot-desktop');
  await page.close();
});

// ---------- screenshots for a visual check (GROWTH_SCREENS=<dir>) ----------

async function shot(page, name) {
  fs.mkdirSync(SCREENS, { recursive: true });
  await page.screenshot({ path: path.join(SCREENS, name + '.png') });
}

test('screenshots of the new UI', { skip: serverSkip || !SCREENS ? 'set GROWTH_SCREENS' : false }, async () => {
  for (const [vp, tag] of [[DESKTOP, 'desktop'], [PHONE, 'phone']]) {
    for (const v of ['a', 'b', 'c']) {
      const p = await openOnline(`${srv.base}/?debug&intro=${v}`, { viewport: vp });
      await p.waitForFunction(() => !document.getElementById('introCopy').classList.contains('pending'));
      await wait(400);
      await shot(p, `intro-${v}-${tag}`);
      await p.close();
    }
    const p = await openOnline(`${srv.base}/?debug&exp=t2_onboarding:b`, { viewport: vp });
    await p.click('#introCta');
    await p.waitForSelector('#onbMarker:not([hidden])');
    await wait(300);
    await shot(p, `onboarding-${tag}`);
    await watchOwnReaction(p);
    await p.waitForSelector('#rate:not([hidden])', { timeout: 20000 });
    await shot(p, `rating-${tag}`);
    await p.click('#rate [data-v="-1"]');
    await p.waitForSelector('#rateForm:not([hidden])');
    await shot(p, `rating-comment-${tag}`);
    await p.click('#shareBtn');
    await p.waitForFunction(() => document.getElementById('shareUrl').value);
    await p.evaluate(() => document.getElementById('share').scrollIntoView());
    await shot(p, `panel-${tag}`);
    assert.ok(await p.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${tag}: no horizontal scroll`);
    await p.click('#doorRoom');
    await wait(200);
    await shot(p, `modal-room-${tag}`);
    await p.keyboard.press('Escape');
    await p.click('#doorPro');
    await wait(200);
    await shot(p, `modal-pro-${tag}`);
    await p.click('#proBuy');
    await shot(p, `modal-pro-wait-${tag}`);
    await p.keyboard.press('Escape');
    const url = await p.inputValue('#shareUrl');
    await p.close();
    const r = await openOnline(url, { viewport: vp, debug: false });
    await r.waitForSelector('#rbar:not([hidden])', { timeout: 60000 });
    await wait(1500);
    await shot(r, `replay-${tag}`);
    assert.ok(await r.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${tag}: no horizontal scroll in the replay`);
    await r.close();
  }
  for (const p of [shared?.page, shared?.replayPage]) await p?.close();
});
