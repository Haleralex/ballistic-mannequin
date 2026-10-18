// Link-preview images for /r/<id> (Open Graph, 1200 × 630): assets/og/<kind>.png for every incident kind of the
// simulator and assets/og/default.png. For each kind the simulator is driven headlessly (tests/lib/harness.mjs):
// a shot that produces that kind is searched for, the reaction runs 3 s, the scene is captured in the dark theme
// and composed with a dark gradient and a Russian caption, then stored as a 256-colour PNG (under ~200 KB).
//
//   cd tests && npm install
//   CHROMIUM_PATH=… node ../tools/og/make.mjs [kind …]
//
// The search is deterministic (fixed seeds), so the images change only when the simulator does.
import fs from 'node:fs';
import path from 'node:path';
import zlib from 'node:zlib';
import { launch, openSim, steps, ROOT } from '../../tests/lib/harness.mjs';

const OUT = path.join(ROOT, 'assets/og');
const W = 1200, H = 630, LIMIT = 200 * 1024;
export const CAPTIONS = {
  near: ['Пролёт пули рядом', 'Попадания нет, но тело реагирует: рефлекс испуга, голова втягивается, руки к лицу.'],
  heart: ['Ранение сердца', 'Кровообращение падает сразу: сознание уходит примерно за 10–15 секунд.'],
  chest: ['Ранение груди', 'Падение при ранении корпуса обычно наступает с задержкой или не наступает вовсе.'],
  abdomen: ['Ранение живота', 'Внутреннее кровотечение почти не видно снаружи, реакция нарастает постепенно.'],
  spine: ['Ранение позвоночника', 'Повреждён спинной мозг: тело теряет опору сразу.'],
  neck: ['Ранение шеи', 'Рука к ране, быстрая кровопотеря.'],
  'head-graze': ['Касательное ранение головы', 'Голова отдёргивается, рука тянется к ране.'],
  pelvis: ['Ранение таза с переломом', 'Повреждена кость таза: ноги перестают держать.'],
  'pelvis-soft': ['Ранение таза', 'Кость цела: человек может стоять и даже идти.'],
  'leg-bone': ['Ранение ноги с переломом', 'Сломана кость: опереться на ногу уже нельзя.'],
  leg: ['Ранение ноги', 'Кость цела: хромота, вес на здоровой ноге.'],
  arm: ['Ранение руки', 'Рука повисает, другая прижимает рану, ноги держат.'],
  multi: ['Несколько попаданий', 'Картечь: реакцию ведёт самое тяжёлое ранение.'],
  default: ['Как тело реагирует на ранение', 'Без киношного отлёта назад: реакцию определяет задетый орган, а не сила удара.'],
};
const KINDS = Object.keys(CAPTIONS).filter(k => k !== 'default');

const FRONT = [0.9, 1.45, 4.2], BACK = [-0.4, 1.4, -4], SIDE = [3.2, 1.5, 1.2];
// where to look first for each kind; the search then widens to every part, offset, side and weapon
const HINTS = {
  near: [{ part: 'head', dx: 0.35 }], heart: [{ part: 'chest', dx: 0.03, dy: -0.03, weapon: 'rifle' }],
  chest: [{ part: 'chest', dx: -0.07 }], abdomen: [{ part: 'abdomen', dx: 0.04 }],
  spine: [{ part: 'abdomen', from: BACK }, { part: 'chest', from: BACK }], neck: [{ part: 'head', dy: -0.11 }],
  'head-graze': [{ part: 'head', dx: 0.06 }], pelvis: [{ part: 'pelvis', dx: 0.08 }], 'pelvis-soft': [{ part: 'pelvis', dx: 0.02, dy: 0.04 }],
  'leg-bone': [{ part: 'thighL' }], leg: [{ part: 'thighL', dx: 0.06 }], arm: [{ part: 'uArmL' }], multi: [{ part: 'chest', weapon: 'buck' }],
};
const PARTS = ['chest', 'abdomen', 'pelvis', 'head', 'thighL', 'shinR', 'uArmL', 'fArmR'];
function* candidates(kind) {
  yield* HINTS[kind] || [];
  for (const weapon of ['pistol', 'rifle', 'buck'])
    for (const from of [FRONT, SIDE, BACK])
      for (const part of PARTS)
        for (const dy of [0, -0.04, 0.04, -0.09, 0.09])
          for (const dx of [0, 0.03, -0.03, 0.07, -0.07, 0.35]) yield { part, dx, dy, from, weapon };
}

// in the page: reset with a seed, set the weapon, fire along a ray at the part; returns the shot's announcement
async function install(page) {
  await page.evaluate(async () => {
    const bus = await import('/bus.js');
    bus.on('shot', d => { window.__lastShot = d; });
    window.__try = ({ seed, part, dx = 0, dy = 0, from, weapon = 'pistol' }) => {
      const s = window.__sim;
      s.setTime('t0');
      s.reset(seed);
      s.setting('weapon', weapon); s.setting('react', 'stay'); s.setting('impulse', 'real'); s.setting('bleed', 1);
      for (let i = 0; i < 60; i++) s.step(); // settle on the feet
      const t = s.byName()[part].body.translation(), o = from || [0.9, 1.45, 4.2];
      const d = [t.x + dx - o[0], t.y + dy - o[1], t.z - o[2]], l = Math.hypot(...d);
      window.__lastShot = null;
      s.fire(o, d.map(v => v / l));
      return window.__lastShot;
    };
  });
}

// frame the body (standing or down) on the right of the picture, slightly from above
async function frame(page) {
  await page.evaluate(() => {
    const s = window.__sim, p = s.byName().pelvis.body.translation(), c = s.byName().chest.body.translation();
    const tx = (p.x + c.x) / 2, tz = (p.z + c.z) / 2, ty = Math.max(0.55, Math.min(1.1, (p.y + c.y) / 2));
    s.camera.position.set(tx + 1.15, ty + 0.55, tz + 2.6);
    s.controls.target.set(tx - 0.55, ty - 0.05, tz);
    s.controls.update();
  });
}

async function capture(browser, kind) {
  const page = await openSim(browser, { viewport: { width: W + 360, height: H } });
  await page.addStyleTag({ content: '#stage > :not(canvas) { visibility: hidden !important; }' }); // chips, hints, captions
  await page.evaluate(() => { localStorage.setItem('mq_intro', '1'); document.getElementById('intro').hidden = true; document.getElementById('app').inert = false; });
  await page.evaluate(() => { document.documentElement.dataset.theme = 'dark'; document.getElementById('qHigh').click(); window.__sim.renderer.setPixelRatio(1.5); });
  await install(page);
  let found = null;
  if (kind !== 'default') {
    let n = 0;
    for (const c of candidates(kind)) {
      const shot = await page.evaluate(c => window.__try(c), { ...c, seed: 1000 + (n++ % 7) });
      if (shot?.kind === kind) { found = { ...c, seed: 1000 + ((n - 1) % 7) }; break; }
      if (n > 1500) break;
    }
    if (!found) { await page.close(); throw new Error(`no shot found for ${kind}`); }
    await steps(page, 360); // 3 s of the reaction
  } else {
    await steps(page, 120);
  }
  await frame(page);
  await page.waitForTimeout(1500); // tracers and dust fade in real time; let a few frames draw
  const png = await page.locator('#c').screenshot();
  await page.close();
  return { png, found };
}

async function compose(browser, kind, png) {
  const [title, line] = CAPTIONS[kind];
  const page = await browser.newPage({ viewport: { width: W, height: H } });
  await page.setContent(`<!doctype html><meta charset="utf-8"><style>
    * { box-sizing: border-box; margin: 0; }
    body { width: ${W}px; height: ${H}px; overflow: hidden; background: #13171a; font-family: "Barlow Condensed", "Inter Display", Inter, "DejaVu Sans", sans-serif; }
    img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
    .shade { position: absolute; inset: 0; background: linear-gradient(90deg, rgba(13,17,20,.96) 0%, rgba(13,17,20,.9) 34%, rgba(13,17,20,.35) 62%, rgba(13,17,20,0) 80%),
      linear-gradient(0deg, rgba(13,17,20,.75) 0%, rgba(13,17,20,0) 30%); }
    .text { position: absolute; left: 64px; top: 0; bottom: 0; width: 560px; display: flex; flex-direction: column; justify-content: center; color: #e2e7e9; }
    .brand { font: 600 22px Inter, "DejaVu Sans", sans-serif; letter-spacing: .14em; text-transform: uppercase; color: #6c9cff; margin-bottom: 22px; }
    h1 { font-weight: 700; font-size: 66px; line-height: 1.02; text-transform: uppercase; letter-spacing: .005em; }
    p { font: 400 26px/1.35 Inter, "DejaVu Sans", sans-serif; color: #b9c3c8; margin-top: 22px; }
    .foot { position: absolute; left: 64px; bottom: 40px; font: 500 20px Inter, "DejaVu Sans", sans-serif; color: #95a2a8; }
  </style><img src="data:image/png;base64,${png.toString('base64')}"><div class="shade"></div>
  <div class="text"><div class="brand">Баллистический манекен</div><h1>${title}</h1><p>${line}</p></div>
  <div class="foot">Физическая модель · справочник для актёров, аниматоров и авторов</div>`);
  await page.evaluate(() => document.fonts.ready);
  // a full-colour PNG of a rendered scene runs to ~500 KB; a 256-colour palette PNG of the same picture is a
  // third of that and looks the same here (dark greys and the mannequin's few tones)
  await page.evaluate(() => document.querySelector('img').decode());
  const shot = await page.screenshot({ type: 'png' });
  if (shot.length <= LIMIT) { await page.close(); return shot; }
  const rgba = Buffer.from(await page.evaluate(async src => {
    const img = new Image();
    img.src = 'data:image/png;base64,' + src;
    await img.decode();
    const c = Object.assign(document.createElement('canvas'), { width: img.width, height: img.height });
    const ctx = c.getContext('2d');
    ctx.drawImage(img, 0, 0);
    const a = ctx.getImageData(0, 0, c.width, c.height).data;
    let bin = '';
    for (let i = 0; i < a.length; i += 0x8000) bin += String.fromCharCode(...a.subarray(i, i + 0x8000));
    return btoa(bin);
  }, shot.toString('base64')), 'base64');
  await page.close();
  return palettePng(rgba, W, H, 256);
}

// median cut: split the box of colours with the widest channel at its median until there are n boxes; each box's
// mean is a palette entry. Pixels map to the nearest entry (cached per colour).
function palettePng(rgba, w, h, n) {
  const px = w * h, cols = new Uint32Array(px);
  for (let i = 0; i < px; i++) cols[i] = (rgba[i * 4] << 16) | (rgba[i * 4 + 1] << 8) | rgba[i * 4 + 2];
  const ch = (c, k) => (c >> (16 - 8 * k)) & 255;
  const range = box => {
    let best = 0, bk = 0;
    for (let k = 0; k < 3; k++) {
      let lo = 255, hi = 0;
      for (const c of box) { const v = ch(c, k); if (v < lo) lo = v; if (v > hi) hi = v; }
      if (hi - lo > best) { best = hi - lo; bk = k; }
    }
    return [best, bk];
  };
  let boxes = [cols];
  while (boxes.length < n) {
    let bi = -1, bw = 0, bk = 0;
    boxes.forEach((b, i) => {
      if (b.length < 2) return;
      const [r, k] = range(b), score = r * Math.log2(b.length + 1);
      if (score > bw) { bw = score; bi = i; bk = k; }
    });
    if (bi < 0) break;
    const b = Uint32Array.from(boxes[bi]).sort((x, y) => ch(x, bk) - ch(y, bk)), m = b.length >> 1;
    boxes.splice(bi, 1, b.subarray(0, m), b.subarray(m));
  }
  const pal = boxes.map(b => { const s = [0, 0, 0]; for (const c of b) for (let k = 0; k < 3; k++) s[k] += ch(c, k); return s.map(v => Math.round(v / b.length)); });
  const cache = new Map(), idx = Buffer.alloc((w + 1) * h);
  for (let y = 0; y < h; y++) {
    idx[y * (w + 1)] = 0; // filter: none
    for (let x = 0; x < w; x++) {
      const c = cols[y * w + x];
      let j = cache.get(c);
      if (j === undefined) {
        let bd = Infinity;
        pal.forEach((p, i) => { const d = (p[0] - ch(c, 0)) ** 2 * 2 + (p[1] - ch(c, 1)) ** 2 * 4 + (p[2] - ch(c, 2)) ** 2 * 3; if (d < bd) { bd = d; j = i; } });
        cache.set(c, j);
      }
      idx[y * (w + 1) + 1 + x] = j;
    }
  }
  const chunk = (type, data) => {
    const len = Buffer.alloc(4), crc = Buffer.alloc(4), td = Buffer.concat([Buffer.from(type, 'ascii'), data]);
    len.writeUInt32BE(data.length);
    crc.writeUInt32BE(zlib.crc32(td));
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 3; // 8-bit indices into a palette
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('PLTE', Buffer.from(pal.flat())),
    chunk('IDAT', zlib.deflateSync(idx, { level: 9 })), chunk('IEND', Buffer.alloc(0))]);
}

const only = process.argv.slice(2);
const browser = await launch();
fs.mkdirSync(OUT, { recursive: true });
for (const kind of ['default', ...KINDS]) {
  if (only.length && !only.includes(kind)) continue;
  const { png, found } = await capture(browser, kind);
  const out = await compose(browser, kind, png);
  fs.writeFileSync(path.join(OUT, kind + '.png'), out);
  console.log(`${kind}.png ${(out.length / 1024).toFixed(0)} KB${found ? ` · ${found.weapon || 'pistol'} at ${found.part} (${found.dx || 0}, ${found.dy || 0})${found.from === BACK ? ' from behind' : found.from === SIDE ? ' from the side' : ''}` : ''}`);
}
await browser.close();
