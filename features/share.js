// "Поделиться": the case of this session (sim.getCase) goes to POST /api/v1/cases, the link /r/<id> is copied and
// shown. A page opened with ?case=<id> fetches that case, plays it back with a replay bar (progress, pause, seek)
// and "Сделать свой выстрел" (sim.takeOver). Events: share_link, share_open, share_convert (the viewer's first own
// shot after opening someone's link).
import { on, sim } from '../bus.js';
import { online, post, get, visitorId } from './api.js';
import { track } from './analytics.js';
import { $, toast, copyText, status, shield } from './ui.js';

const CASE_ID = /^[0-9A-Za-z]{8}$/;
let opened = null; // the shared case this page opened: { id, c }
let converted = false;
const ready = () => new Promise(r => { if (sim.getCase) r(); else on('ready', r); });

export function init({ caseId }) {
  shield($('share'));
  $('shareBtn').addEventListener('click', share);
  $('shareCopy').addEventListener('click', async () => {
    const ok = await copyText($('shareUrl').value, $('shareUrl'));
    status($('shareNote'), ok ? 'Ссылка скопирована.' : 'Не получилось скопировать: выделите ссылку и скопируйте вручную.', ok ? 'ok' : 'err');
  });
  $('shareUrl').addEventListener('focus', e => e.target.select());
  on('shot', () => {
    if (!opened || converted || sim.replaying) return;
    converted = true;
    track('share_convert', { case_id: opened.id });
  });
  if (caseId != null) openShared(caseId);
}

async function share() {
  const btn = $('shareBtn'), note = $('shareNote');
  if (!sim.getCase) return status(note, 'Симулятор ещё загружается.', 'err');
  // someone's case still playing back: its own link is the one to pass on
  if (opened && sim.replaying) {
    track('share_link', { case_id: opened.id, kind: opened.c.meta?.kind ?? null });
    return present(`${location.origin}/r/${opened.id}`);
  }
  const c = sim.getCase();
  if (!c.events.some(e => e.type === 'shot')) return status(note, 'Сначала выстрелите: пока делиться нечем.', 'err');
  btn.disabled = true;
  status(note, 'Сохраняю реакцию…');
  try {
    const r = await post('/cases', { visitor_id: visitorId(), case: c });
    track('share_link', { case_id: r.id, kind: c.meta?.kind ?? null });
    await present(`${location.origin}/r/${r.id}`);
  } catch (e) {
    status(note, e.message, 'err');
  } finally { btn.disabled = false; }
}

async function present(url) {
  const field = $('shareUrl');
  $('shareOut').hidden = false;
  field.value = url;
  const ok = await copyText(url, field);
  field.setSelectionRange(url.length, url.length);
  field.scrollLeft = field.scrollWidth; // the id at the end is the part that differs
  status($('shareNote'), ok ? 'Ссылка скопирована.' : 'Скопируйте ссылку из поля выше.', 'ok');
}

// ---------- a shared link: ?case=<id> ----------
async function openShared(id) {
  if (!await online) return toast('Повтор по ссылке открывается только на сайте симулятора.', { kind: 'err', ms: 0 });
  if (!CASE_ID.test(id)) return toast('Ссылка повреждена: такой реакции нет.', { kind: 'err', ms: 0 });
  toast('Загружаю реакцию…', { ms: 0 });
  let c;
  try { c = await get('/cases/' + id); } catch (e) { return toast(e.message, { kind: 'err', ms: 0 }); }
  await ready();
  try { await sim.loadCase(c); } catch (e) { return toast(e.message, { kind: 'err', ms: 0 }); }
  toast('');
  opened = { id, c };
  track('share_open', { case_id: id });
  replayBar(c);
}

const ICON = {
  pause: ['Пауза', '<svg viewBox="0 0 16 16" aria-hidden="true"><rect x="3" y="2" width="3.5" height="12" rx="1"/><rect x="9.5" y="2" width="3.5" height="12" rx="1"/></svg>'],
  play: ['Продолжить', '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 2.2v11.6c0 .6.7 1 1.2.6l8.6-5.8c.4-.3.4-1 0-1.3L5.2 1.6C4.7 1.2 4 1.6 4 2.2z"/></svg>'],
  again: ['Сначала', '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 2.5a5.5 5.5 0 1 1-5.3 7h1.6A4 4 0 1 0 8 4v2L4.8 3.2 8 .5z"/></svg>'],
};

function replayBar(c) {
  const box = $('rbar'), seek = $('rbarSeek'), time = $('rbarTime'), play = $('rbarPlay'), take = $('rbarTake'), stage = $('stage');
  const fmt = s => (s / 120).toFixed(1).replace('.', ',');
  const total = fmt(c.steps);
  const paused = () => $('t0').getAttribute('aria-pressed') === 'true';
  const resume = () => { if (paused()) $('t1').click(); };
  let dragging = false, ended = false, active = true;

  $('rbarTitle').textContent = c.meta?.title || 'Реакция';
  seek.max = c.steps; seek.value = 0;
  box.hidden = false;
  stage.classList.add('has-rbar');
  shield(box);

  const tick = () => {
    if (!active) return;
    if (!dragging) seek.value = sim.step;
    time.textContent = `${fmt(dragging ? +seek.value : sim.step)} / ${total} с`;
    const s = ended ? 'again' : paused() ? 'play' : 'pause';
    if (play.dataset.state !== s) { play.dataset.state = s; play.innerHTML = ICON[s][1]; play.setAttribute('aria-label', ICON[s][0]); play.title = ICON[s][0]; }
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  on('replay-end', () => { ended = true; });

  play.addEventListener('click', async () => {
    if (!sim.replaying) return;
    if (ended) { ended = false; await sim.seek(0); resume(); return; }
    $(paused() ? 't1' : 't0').click();
  });
  seek.addEventListener('input', () => { dragging = true; });
  seek.addEventListener('change', async () => {
    dragging = false; ended = false;
    await sim.seek(+seek.value);
  });
  take.addEventListener('click', () => {
    sim.takeOver();
    close();
    resume();
    toast('Теперь стреляете вы: клик по манекену — выстрел.', { ms: 6000 });
  });
  // clicks on the scene do not shoot during a replay: say why, and point at the button
  const cv = $('c');
  let down = null;
  cv.addEventListener('pointerdown', e => { down = e.button === 0 && !e.shiftKey ? [e.clientX, e.clientY] : null; });
  cv.addEventListener('pointerup', e => {
    if (active && sim.replaying && down && Math.hypot(e.clientX - down[0], e.clientY - down[1]) < 6) {
      take.classList.remove('nudge'); void take.offsetWidth; take.classList.add('nudge');
      toast('Это повтор чужой реакции. Чтобы стрелять самому, нажмите «Сделать свой выстрел».', { ms: 4000 });
    }
    down = null;
  });
  on('reset', close); // R or "Сбросить манекен" ends the replay too
  function close() {
    active = false;
    box.hidden = true;
    stage.classList.remove('has-rbar');
  }
}
