// T2 (t2_onboarding). Variant a: the empty sandbox as it was. Variant b: a hint "Кликните по груди манекена — это
// выстрел" with a marker on the chest, then, after the first own shot, one line on what is happening. Shown until
// the first hit, once per visitor (mq_onb).
//
// The simulator does not tell where the chest is on screen, so the marker is placed by projecting the chest's rest
// position through the starting camera (app.js: camera at 0.9, 1.45, 4.2 looking at 0, 1, 0, fov 42°). Once the
// viewer turns or zooms the camera that is no longer true, and the marker goes (the hint stays).
import { on, sim } from '../bus.js';
import { local } from './api.js';
import { $ } from './ui.js';

const CAM = [0.9, 1.45, 4.2], TARGET = [0, 1, 0], FOV = 42, CHEST = [0, 1.36, 0.1];

// the chest's point on the stage in CSS pixels, for the starting camera and the stage's current size
export function chestPoint(w, h) {
  const sub = (a, b) => a.map((v, i) => v - b[i]), dot = (a, b) => a.reduce((s, v, i) => s + v * b[i], 0);
  const cross = (a, b) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
  const norm = a => { const l = Math.hypot(...a); return a.map(v => v / l); };
  const f = norm(sub(TARGET, CAM)), r = norm(cross(f, [0, 1, 0])), u = cross(r, f), d = sub(CHEST, CAM);
  const t = Math.tan(FOV * Math.PI / 360), z = dot(d, f);
  const nx = dot(d, r) / (z * t * (w / h)), ny = dot(d, u) / (z * t);
  return [(nx + 1) / 2 * w, (1 - ny) / 2 * h];
}

export function start() {
  if (local.get('mq_onb')) return;
  const stage = $('stage'), marker = $('onbMarker'), tip = $('onbTip'), cv = $('c');
  let moved = false, done = false;
  const place = () => {
    if (done) return;
    const w = stage.clientWidth, h = stage.clientHeight, [x, y] = chestPoint(w, h);
    marker.style.left = x + 'px'; marker.style.top = y + 'px';
    marker.hidden = moved;
    // the tip sits to the right of the chest, or under the chips once the marker is gone
    if (moved) { tip.style.left = ''; tip.style.top = ''; tip.classList.add('top'); }
    else {
      tip.classList.remove('top');
      tip.style.left = Math.min(x + 30, w - 16 - Math.min(260, w - x - 46)) + 'px';
      tip.style.top = (y - 18) + 'px';
      tip.style.maxWidth = Math.max(140, Math.min(260, w - x - 46)) + 'px';
    }
  };
  tip.textContent = 'Кликните по груди манекена — это выстрел';
  tip.hidden = false;
  place();
  const ro = new ResizeObserver(place);
  ro.observe(stage);
  // the camera turned or zoomed: the projected point no longer matches
  let down = null;
  const lost = () => { if (!moved) { moved = true; place(); } };
  cv.addEventListener('pointerdown', e => { down = [e.clientX, e.clientY]; });
  cv.addEventListener('pointermove', e => { if (down && Math.hypot(e.clientX - down[0], e.clientY - down[1]) > 6) lost(); });
  addEventListener('pointerup', () => { down = null; });
  cv.addEventListener('wheel', lost, { passive: true });

  let hideTimer = 0;
  const off = on('shot', d => {
    if (sim.replaying || done) return;
    marker.hidden = true;
    clearTimeout(hideTimer);
    tip.classList.add('top');
    tip.style.left = ''; tip.style.top = ''; tip.style.maxWidth = '';
    const where = matchMedia('(max-width: 820px)').matches ? 'Ниже' : 'Справа';
    if (d.hit) {
      done = true; off(); ro.disconnect();
      local.set('mq_onb', '1');
      tip.textContent = `Пуля почти не толкает тело: реакцию задаёт задетый орган, а не сила удара. ${where} — раневой канал, кровопотеря и сценарий поведения.`;
      hideTimer = setTimeout(() => { tip.hidden = true; }, 12000);
    } else {
      tip.textContent = 'Мимо: пуля прошла рядом, тело только вздрогнуло. Попробуйте попасть в грудь.';
      hideTimer = setTimeout(() => { if (!done) { tip.textContent = 'Кликните по груди манекена — это выстрел'; place(); } }, 5000);
    }
  });
  on('reset', () => { if (done) tip.hidden = true; });
}
