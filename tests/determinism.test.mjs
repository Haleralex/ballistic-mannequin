// Determinism: a case (seed + inputs stamped with physics steps) replays the same reaction, step for step.
// About twenty varied sessions are recorded through the ?debug hook, then played back from their case in the same
// page (with frames drawn in between and the camera moved), with a seek, and twice in a fresh page. The state hash
// must match the recording every 120 steps, and at every check the case carries.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { launch, openSim, steps, shootAt, ROOT } from './lib/harness.mjs';

const EVERY = 120; // the tests compare more often than the case's own checks (every 1200)
const FRONT = [0.9, 1.45, 4.2], SIDE = [3.2, 1.5, 1.2], BACK = [-0.4, 1.4, -4];
// a session: settings at step 0, shots at a step aimed at a body part (offset dx, dy in metres), setting changes
const SESSIONS = [
  { name: 'pistol, chest', seed: 1, react: 'stay', steps: 2400, shots: [{ at: 120, part: 'chest' }] },
  { name: 'pistol, abdomen', seed: 2, react: 'stay', steps: 2400, shots: [{ at: 120, part: 'abdomen', dx: 0.04 }] },
  { name: 'pistol, head', seed: 3, steps: 1800, shots: [{ at: 120, part: 'head' }] },
  { name: 'pistol, left thigh', seed: 4, react: 'stay', steps: 2400, shots: [{ at: 120, part: 'thighL' }] },
  { name: 'pistol, right arm', seed: 5, react: 'stay', steps: 2400, shots: [{ at: 120, part: 'uArmR' }] },
  { name: 'rifle, heart', seed: 6, weapon: 'rifle', steps: 2400, shots: [{ at: 120, part: 'chest', dx: 0.03, dy: -0.03 }] },
  { name: 'rifle, abdomen from the side', seed: 7, weapon: 'rifle', react: 'stay', steps: 2400, shots: [{ at: 120, part: 'abdomen', from: SIDE }] },
  { name: 'buckshot, chest', seed: 8, weapon: 'buck', steps: 2400, shots: [{ at: 120, part: 'chest' }] },
  { name: 'buckshot, legs', seed: 9, weapon: 'buck', react: 'stay', steps: 2400, shots: [{ at: 120, part: 'thighR' }] },
  { name: 'slug, pelvis', seed: 10, weapon: 'slug', steps: 2400, shots: [{ at: 120, part: 'pelvis' }] },
  { name: 'near miss past the head', seed: 11, react: 'stay', steps: 1800, shots: [{ at: 120, part: 'head', dx: 0.35 }] },
  { name: 'into the floor by the feet', seed: 12, react: 'stay', steps: 1800, shots: [{ at: 120, part: 'footL', dx: 0.5, dy: -0.2 }] },
  { name: 'flee: arm hit, then a hit while running', seed: 13, react: 'flee', steps: 3600, shots: [{ at: 60, part: 'uArmL' }, { at: 420, part: 'chest', running: true }] },
  { name: 'flee: near miss, then buckshot while running', seed: 14, react: 'flee', steps: 3600,
    shots: [{ at: 60, part: 'head', dx: 0.4 }, { at: 400, part: 'abdomen', running: true }], changes: [{ at: 300, key: 'weapon', value: 'buck' }] },
  { name: 'bleeding ×10 from the start', seed: 15, bleed: 10, react: 'stay', steps: 3600, shots: [{ at: 120, part: 'abdomen' }] },
  { name: 'bleeding ×10 switched on and off', seed: 16, steps: 3600, shots: [{ at: 120, part: 'thighL', dx: 0.03 }],
    changes: [{ at: 600, key: 'bleed', value: 10 }, { at: 1800, key: 'bleed', value: 1 }] },
  { name: 'two shots, weapon changed between', seed: 17, react: 'stay', steps: 3600, shots: [{ at: 120, part: 'chest' }, { at: 1300, part: 'head' }],
    changes: [{ at: 900, key: 'weapon', value: 'rifle' }] },
  { name: 'movie impulse, chest', seed: 18, impulse: 'movie', steps: 2400, shots: [{ at: 120, part: 'chest' }] },
  { name: 'shot in the back', seed: 19, steps: 2400, shots: [{ at: 120, part: 'chest', from: BACK }] },
  { name: 'auto reaction, arm, then the leg', seed: 20, steps: 3600, shots: [{ at: 90, part: 'fArmL' }, { at: 1500, part: 'shinR' }] },
  { name: 'a shot at step 0', seed: 0xfffffff0, steps: 1200, shots: [{ at: 0, part: 'chest' }] },
];

// page-side helpers: record a session, replay a case; both collect the state hash every EVERY steps
function install(page) {
  return page.evaluate(EVERY => {
    const s = window.__sim, sim = s.sim;
    const aim = (part, dx = 0, dy = 0, from) => {
      const t = s.byName()[part].body.translation(), o = from || [0.9, 1.45, 4.2];
      const d = [t.x + dx - o[0], t.y + dy - o[1], t.z - o[2]], l = Math.hypot(...d);
      s.fire(o, d.map(v => v / l));
    };
    const tick = (hashes) => { s.step(); if (sim.step % EVERY === 0) hashes.push([sim.step, sim.stateHash()]); };
    window.__record = spec => {
      s.setTime('t0');
      s.reset(spec.seed);
      const D = { weapon: 'pistol', impulse: 'real', react: 'auto', bleed: 1 }; // settings carry over a reset: set them all
      for (const k in D) s.setting(k, spec[k] ?? D[k]);
      const acts = [...(spec.shots || []).map(x => ({ ...x, shot: true })), ...(spec.changes || [])].sort((a, b) => a.at - b.at);
      const hashes = [], notes = [];
      let i = 0;
      // the inputs of a step go in right after it, before its hash is read: a replay applies them at the same point
      const due = () => {
        while (i < acts.length && acts[i].at === sim.step) {
          const a = acts[i++];
          if (a.shot) { notes.push({ running: !!(s.character().run && s.character().run.mode === 'go'), wounds: s.character().wounds.length }); aim(a.part, a.dx, a.dy, a.from); }
          else s.setting(a.key, a.value);
        }
      };
      due();
      while (sim.step < spec.steps) { s.step(); due(); if (sim.step % EVERY === 0) hashes.push([sim.step, sim.stateHash()]); }
      const c = s.character();
      return { c: sim.getCase(), hashes, notes, wounds: c.wounds.map(w => w.zone), lost: c.lost, physT: c.physT, state: c.state, script: s.script()?.title || null };
    };
    // replay a case: from step 0, or seek to `from` first. `frames`: let the page draw between slices of steps
    window.__replay = async (c, { from = 0, frames = false, camera } = {}) => {
      s.setTime('t0');
      if (camera) { s.camera.position.set(...camera); s.controls.update(); }
      const hashes = [];
      await sim.loadCase(c);
      if (!sim.replaying || sim.step !== 0) throw new Error('loadCase did not start a replay at step 0');
      if (from) { await sim.seek(from); if (sim.step !== from) throw new Error(`seek landed on ${sim.step}`); }
      while (sim.step < c.steps) {
        tick(hashes);
        if (frames && sim.step % 120 === 0) await new Promise(r => requestAnimationFrame(() => r()));
      }
      return { hashes, checks: s.checks(), replaying: sim.replaying };
    };
  }, EVERY);
}

let browser, page, recorded;
before(async () => {
  browser = await launch();
  page = await openSim(browser);
  await install(page);
});
after(async () => { await browser?.close(); });

test('varied sessions record well-formed cases', async () => {
  recorded = [];
  for (const spec of SESSIONS) {
    const r = await page.evaluate(spec => window.__record(spec), spec);
    recorded.push({ spec, ...r });
    const c = r.c;
    assert.equal(c.v, 1);
    assert.equal(c.seed, spec.seed >>> 0, spec.name);
    assert.equal(c.steps, spec.steps, spec.name);
    assert.equal(typeof c.build, 'string');
    assert.ok(c.events.every((e, i) => i === 0 || e.step >= c.events[i - 1].step), `${spec.name}: events sorted by step`);
    const initial = c.events.filter(e => e.step === 0 && e.type === 'setting').map(e => e.key).sort();
    assert.deepEqual(initial, ['bleed', 'impulse', 'react', 'weapon'], `${spec.name}: the settings at step 0`);
    const shots = c.events.filter(e => e.type === 'shot');
    assert.equal(shots.length, spec.shots.length, `${spec.name}: every shot is recorded`);
    assert.deepEqual(shots.map(e => e.step), spec.shots.map(x => x.at));
    for (const e of shots) assert.ok(Math.abs(Math.hypot(...e.dir) - 1) < 1e-5, 'a shot direction is a unit vector');
    for (const ch of spec.changes || []) assert.ok(c.events.some(e => e.type === 'setting' && e.step === ch.at && e.key === ch.key && e.value === ch.value), `${spec.name}: the change of ${ch.key} at ${ch.at}`);
    assert.equal(c.check.length, Math.floor(spec.steps / 1200), `${spec.name}: a check every 1200 steps`);
    const byStep = new Map(r.hashes);
    for (const k of c.check) assert.equal(k.hash, byStep.get(k.step), `${spec.name}: check at ${k.step}`);
    assert.ok(new TextEncoder().encode(JSON.stringify(c)).length <= 64 * 1024, 'within 64 KB');
    assert.ok(c.meta && typeof c.meta.title === 'string' && Array.isArray(c.meta.zones) && Array.isArray(c.meta.organs));
  }
  // the sessions cover what they claim: hits in every region, misses, a hit while running, the bleed speed-up
  const zones = recorded.flatMap(r => r.wounds).join(' ');
  for (const z of ['грудная', 'живот', 'голова|шея', 'бедро', 'плечо|предплечье', 'таз']) assert.match(zones, new RegExp(z), `some session wounds: ${z}`);
  const by = name => recorded.find(r => r.spec.name === name);
  assert.equal(by('near miss past the head').wounds.length, 0);
  assert.equal(by('near miss past the head').c.meta.kind, 'near');
  assert.ok(by('flee: arm hit, then a hit while running').notes[1].running, 'the second shot lands while running');
  assert.ok(by('flee: near miss, then buckshot while running').notes[1].running, 'the buckshot lands while running');
  // physiology runs ten times faster while the speed-up is on: 3600 steps are 300 s, or 5 + 100 + 15 s when switched
  assert.ok(Math.abs(by('bleeding ×10 from the start').physT - 300) < 0.01, 'the speed-up from step 0');
  assert.ok(Math.abs(by('bleeding ×10 switched on and off').physT - 120) < 0.01, 'the speed-up between steps 600 and 1800');
  assert.ok(Math.abs(by('pistol, abdomen').physT - 20) < 0.01, 'no speed-up');
  assert.equal(by('buckshot, chest').c.meta.weapon, 'buck');
  assert.deepEqual(page.errors, []);
});

test('every case replays identically in the same page, with frames drawn and the camera moved', async () => {
  assert.ok(recorded, 'recorded sessions');
  for (const [i, r] of recorded.entries()) {
    const out = await page.evaluate(([c, opts]) => window.__replay(c, opts), [r.c, { frames: i % 2 === 0, camera: i % 3 === 0 ? [4, 2.5, -3] : undefined }]);
    assert.deepEqual(out.hashes, r.hashes, `${r.spec.name}: hashes along the replay`);
    assert.deepEqual(out.checks, r.c.check, `${r.spec.name}: checks`);
  }
  assert.deepEqual(page.errors, []);
});

test('seeking replays from 0 and lands on the same state', async () => {
  for (const r of recorded.filter((_, i) => i % 4 === 1)) {
    const from = Math.floor(r.c.steps * 0.55 / 10) * 10;
    const out = await page.evaluate(([c, opts]) => window.__replay(c, opts), [r.c, { from }]);
    assert.deepEqual(out.hashes, r.hashes.filter(([s]) => s > from), `${r.spec.name}: hashes after seeking to ${from}`);
    assert.deepEqual(out.checks, r.c.check, `${r.spec.name}: checks recomputed through the seek`);
  }
});

test('a fresh page replays every case identically, twice', async () => {
  const fresh = await openSim(browser);
  await install(fresh);
  for (const r of recorded) {
    for (const round of [1, 2]) {
      const out = await fresh.evaluate(([c, opts]) => window.__replay(c, opts), [r.c, { camera: round === 2 ? [-3, 1.2, 2] : undefined }]);
      assert.deepEqual(out.hashes, r.hashes, `${r.spec.name}: fresh page, round ${round}`);
    }
  }
  assert.deepEqual(fresh.errors, []);
  await fresh.close();
});

test('while replaying clicks do not shoot and settings hold; after takeOver new inputs go on top of the case', async () => {
  const base = recorded.find(r => r.spec.name === 'pistol, chest');
  const events = [];
  await page.exposeFunction('__bus', e => events.push(e));
  await page.evaluate(async () => {
    const bus = await import('./bus.js');
    for (const n of ['case-loaded', 'replay-end', 'reaction', 'reaction-watched', 'shot']) bus.on(n, d => window.__bus({ n, d: n === 'case-loaded' ? { steps: d.case.steps } : d }));
  });
  await page.evaluate(c => window.__sim.sim.loadCase(c), base.c);
  await steps(page, 600);
  const woundsBefore = await page.evaluate(() => window.__sim.character().wounds.length);
  await shootAt(page, 'head'); // the click path
  await page.evaluate(() => document.getElementById('w-rifle').click());
  const during = await page.evaluate(() => ({ wounds: window.__sim.character().wounds.length, events: window.__sim.sim.getCase().events.length,
    weapon: document.getElementById('w-pistol').getAttribute('aria-pressed'), replaying: window.__sim.sim.replaying }));
  assert.equal(during.wounds, woundsBefore, 'a click during a replay does not shoot');
  assert.equal(during.events, base.c.events.length, 'nor is anything recorded');
  assert.equal(during.weapon, 'true', 'the weapon of the case stays chosen');
  assert.equal(during.replaying, true);

  await steps(page, base.c.steps - 600);
  await new Promise(r => setTimeout(r, 50));
  assert.ok(events.some(e => e.n === 'case-loaded' && e.d.steps === base.c.steps), 'case-loaded');
  assert.equal(events.filter(e => e.n === 'replay-end').length, 1, 'replay-end once');
  const reactions = events.filter(e => e.n === 'reaction'), watched = events.filter(e => e.n === 'reaction-watched');
  assert.ok(reactions.length >= 1 && watched.length === 1, `one scenario watched for 10 s (${reactions.length} started, ${watched.length} watched)`);
  assert.equal(watched[0].d.seconds, 10);
  assert.equal(watched[0].d.id, reactions.at(-1).d.id);

  // take over at the end and shoot again: the case grows from the replayed one and still replays exactly
  await page.evaluate(() => window.__sim.sim.takeOver());
  assert.equal(await page.evaluate(() => window.__sim.sim.replaying), false);
  await page.evaluate(() => document.getElementById('w-rifle').click());
  await shootAt(page, 'abdomen');
  const more = await page.evaluate(n => {
    const s = window.__sim, hashes = [];
    for (let i = 0; i < n; i++) { s.step(); if (s.sim.step % 120 === 0) hashes.push([s.sim.step, s.sim.stateHash()]); }
    return { c: s.sim.getCase(), hashes };
  }, 1200);
  assert.equal(more.c.steps, base.c.steps + 1200);
  assert.deepEqual(more.c.events.slice(0, base.c.events.length), base.c.events, 'the replayed inputs stay');
  const added = more.c.events.slice(base.c.events.length);
  assert.deepEqual(added.map(e => [e.step, e.type, e.key || null]), [[base.c.steps, 'setting', 'weapon'], [base.c.steps, 'shot', null]]);
  assert.equal(more.c.meta.weapon, 'rifle');
  const again = await page.evaluate(c => window.__replay(c), more.c);
  assert.deepEqual(again.hashes.filter(([s]) => s > base.c.steps), more.hashes);
  assert.deepEqual(again.checks, more.c.check);
  assert.deepEqual(page.errors, []);
});

test('a session with more than 200 inputs gives a shorter case that still replays exactly', async () => {
  const r = await page.evaluate(() => {
    const s = window.__sim, sim = s.sim, hashes = [];
    s.setTime('t0'); s.reset(77);
    for (let i = 0; i < 300; i++) {
      s.step();
      if (i % 50 === 10) { const t = s.byName().chest.body.translation(), o = [0.9, 1.45, 4.2], d = [t.x + 0.3 - o[0], t.y - o[1], t.z - o[2]], l = Math.hypot(...d); s.fire(o, d.map(v => v / l)); }
      s.setting('weapon', i % 2 ? 'pistol' : 'rifle');
      hashes.push([sim.step, sim.stateHash()]);
    }
    return { c: sim.getCase(), hashes };
  });
  assert.ok(r.c.events.length <= 200, `${r.c.events.length} inputs`);
  assert.ok(r.c.steps < 300 && r.c.steps > 150, `cut at step ${r.c.steps}`);
  assert.ok(r.c.events.every(e => e.step < r.c.steps), 'nothing at or past the cut');
  const out = await page.evaluate(c => window.__replay(c), r.c);
  // the replay's hashes are taken after each step's inputs (every 120 steps); the cut step itself has none applied
  assert.deepEqual(out.hashes, r.hashes.filter(([s]) => s % 120 === 0 && s < r.c.steps));
});

test('loadCase refuses what is not a case v1, in Russian', async () => {
  for (const bad of [{ v: 2, seed: 1, steps: 10, events: [] }, { v: 1, steps: 10, events: [] }, { v: 1, seed: 1, steps: 10, events: [{ step: 3, type: 'teleport' }] },
    { v: 1, seed: 1, steps: 10, events: [{ step: 5, type: 'shot', origin: [0, 0, 0], dir: [0, 0, -1] }, { step: 2, type: 'shot', origin: [0, 0, 0], dir: [0, 0, -1] }] }]) {
    const msg = await page.evaluate(c => window.__sim.sim.loadCase(c).then(() => null, e => e instanceof Error ? e.message : 'not an Error'), bad);
    assert.ok(msg && /[а-яё]/i.test(msg), `rejected with a Russian message: ${msg}`);
  }
});

// Claude's scenarios cannot be computed again: the case keeps them and the replay never asks Claude
test('a scenario written by Claude is recorded and replayed without asking again', async () => {
  const BEATS = { title: 'Тестовый сценарий', temperament: 'сдержанный', beats: [
    { t: 0.3, dur: 2, action: 'look', at: 'wound', intensity: 0.8, note: 'смотрит на рану' },
    { t: 0.8, dur: 6, action: 'clutch', hand: 'both', intensity: 0.9, note: 'зажимает' },
    { t: 1.5, dur: 8, action: 'gasp', intensity: 0.7, note: 'дышит' },
    { t: 4, dur: 6, action: 'kneel', knee: 'L', intensity: 0.8, note: 'на колено' },
    { t: 9, dur: 4, action: 'look', at: 'shooter', intensity: 0.6, note: 'на стрелка' },
    { t: 12, dur: 5, action: 'lie_down', intensity: 0.7, note: 'ложится' },
  ] };
  // a stand-in for claude.ai's sampling, answering after a short real delay (so the script lands some steps later)
  const stub = `<script>window.__aiCalls = 0; window.claude = { use: async () => ({ json: async () => { window.__aiCalls++; await new Promise(r => setTimeout(r, 30)); return ${JSON.stringify(BEATS)}; } }) };</script>`;
  const ai = await openSim(browser, { route: async (r, url) => {
    if (url.pathname !== '/') return false;
    const html = fs.readFileSync(path.join(ROOT, 'index.html'), 'utf8').replace('<head>', '<head>' + stub);
    await r.fulfill({ body: html, contentType: 'text/html' });
    return true;
  } });
  await install(ai);
  const rec = await ai.evaluate(async () => {
    const s = window.__sim, sim = s.sim, hashes = [];
    for (let i = 0; i < 100 && document.getElementById('aiBox').disabled; i++) await new Promise(r => setTimeout(r, 20));
    document.getElementById('aiBox').click();
    s.setTime('t0'); s.reset(4242); s.setting('react', 'stay');
    for (let i = 0; i < 2400; i++) {
      s.step();
      if (sim.step === 120) { const t = s.byName().abdomen.body.translation(), o = [0.9, 1.45, 4.2], d = [t.x - o[0], t.y - o[1], t.z - o[2]], l = Math.hypot(...d); s.fire(o, d.map(v => v / l)); }
      if (sim.step % 120 === 0) hashes.push([sim.step, sim.stateHash()]);
      if (sim.step > 120 && sim.step < 200) await new Promise(r => setTimeout(r, 2)); // let the answer arrive
    }
    return { c: sim.getCase(), hashes, calls: window.__aiCalls, title: s.script()?.title };
  });
  assert.equal(rec.calls, 1, 'Claude was asked once');
  const ev = rec.c.events.find(e => e.type === 'script');
  assert.ok(ev, 'the script is in the case');
  assert.equal(ev.source, 'ai');
  assert.equal(ev.title, BEATS.title);
  assert.ok(ev.step > 120, `applied when it arrived (step ${ev.step})`);
  assert.equal(ev.beats.length, BEATS.beats.length);
  assert.equal(rec.title, BEATS.title);
  for (const round of [1, 2]) {
    const out = await ai.evaluate(c => window.__replay(c), rec.c);
    assert.deepEqual(out.hashes, rec.hashes, `replay ${round} of the case with Claude's script`);
  }
  assert.equal(await ai.evaluate(() => window.__aiCalls), 1, 'the replay did not ask Claude');
  // and a page without Claude at all plays it the same
  const plain = await page.evaluate(c => window.__replay(c), rec.c);
  assert.deepEqual(plain.hashes, rec.hashes);
  assert.deepEqual(ai.errors, []);
  await ai.close();
});
