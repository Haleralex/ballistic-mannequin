import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { launch, openSim, steps, shootAt } from './lib/harness.mjs';

let browser;
before(async () => { browser = await launch(); });
after(async () => { await browser?.close(); });

test('the mannequin stands, takes a shot to the chest and the scenario starts', async () => {
  const page = await openSim(browser);
  const events = [];
  await page.exposeFunction('__record', e => events.push(e));
  await page.evaluate(async () => {
    const bus = await import('./bus.js');
    for (const n of ['shot', 'reaction', 'reset']) bus.on(n, d => window.__record({ n, d }));
  });
  await steps(page, 240);
  const standing = await page.evaluate(() => window.__sim.byName().pelvis.body.translation().y);
  assert.ok(standing > 0.9, `pelvis at ${standing} m after 2 s: the mannequin should stand`);

  await shootAt(page, 'chest');
  await steps(page, 120);
  const shot = events.find(e => e.n === 'shot');
  assert.ok(shot, 'a shot event is emitted');
  assert.equal(shot.d.hit, true);
  assert.ok(events.some(e => e.n === 'reaction'), 'a reaction event is emitted');
  const wounds = await page.evaluate(() => window.__sim.character().wounds.length);
  assert.ok(wounds >= 1, 'the chest shot leaves a wound');

  const pelvis = await page.evaluate(() => window.__sim.byName().pelvis.body.translation());
  assert.ok(Number.isFinite(pelvis.x + pelvis.y + pelvis.z), 'physics stays finite');
  assert.deepEqual(page.errors, []);
});
