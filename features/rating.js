// "Похоже на правду?" after the viewer's own scenario has run about 15 s (reaction-watched comes at 10 s of
// simulated time, then a short pause), with an optional comment: POST /api/v1/ratings and the event rate.
// scenario_id is the library scenario's id, or ai / proc for scenarios written by Claude or composed on the spot.
// Never for a case playing back. Unobtrusive: once per scenario, and not again in a session after two dismissals.
import { on, sim } from '../bus.js';
import { isOnline, post, visitorId, session } from './api.js';
import { track, reaction } from './analytics.js';
import { $, status, shield } from './ui.js';

const DELAY = 5000; // after reaction-watched (10 s of the scenario)
let timer = 0, current = null, dismissed = +session.get('mq_rate_x') || 0;
const asked = new Set((() => { try { const a = JSON.parse(session.get('mq_rated') || '[]'); return Array.isArray(a) ? a : []; } catch { return []; } })());

export function init() {
  const box = $('rate');
  shield(box);
  // the scenario's caption at the bottom of the scene moves up above the card while it is there
  new ResizeObserver(() => $('stage').style.setProperty('--rate-h', box.offsetHeight + 'px')).observe(box);
  on('reaction', () => { clearTimeout(timer); if (!box.contains(document.activeElement)) hide(); });
  on('reaction-watched', () => {
    if (sim.replaying || !reaction.own || !isOnline() || dismissed >= 2 || asked.has(reaction.key)) return;
    const r = { ...reaction };
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (sim.replaying || reaction.key !== r.key || !reaction.own || $('intro') && !$('intro').hidden) return;
      show(r);
    }, DELAY);
  });
  box.querySelectorAll('[data-v]').forEach(b => b.addEventListener('click', () => vote(+b.dataset.v)));
  $('rateClose').addEventListener('click', () => {
    dismissed++; session.set('mq_rate_x', dismissed);
    hide();
  });
  $('rateForm').addEventListener('submit', e => { e.preventDefault(); sendComment(); });
}

function show(r) {
  current = { key: r.key, value: 0 };
  asked.add(r.key); session.set('mq_rated', JSON.stringify([...asked].slice(-50)));
  const box = $('rate');
  box.dataset.step = 'ask';
  $('rateForm').hidden = true;
  $('rateComment').value = '';
  status($('rateMsg'), '');
  box.querySelectorAll('[data-v]').forEach(b => { b.disabled = false; b.setAttribute('aria-pressed', 'false'); });
  box.hidden = false;
  $('stage').classList.add('has-rate');
}
function hide() { $('rate').hidden = true; $('stage').classList.remove('has-rate'); current = null; }

async function vote(value) {
  if (!current) return;
  const box = $('rate'), msg = $('rateMsg');
  current.value = value;
  box.querySelectorAll('[data-v]').forEach(b => { b.disabled = true; b.setAttribute('aria-pressed', String(+b.dataset.v === value)); });
  try {
    await post('/ratings', { visitor_id: visitorId(), scenario_id: current.key, case_id: null, value });
    track('rate', { scenario_id: current.key, value });
    box.dataset.step = 'comment';
    $('rateForm').hidden = false;
    status(msg, value > 0 ? 'Спасибо! Можно добавить, что выглядит верно.' : 'Спасибо! Что выглядит не так? Можно коротко.');
    $('rateComment').focus({ preventScroll: true });
  } catch (e) {
    box.querySelectorAll('[data-v]').forEach(b => { b.disabled = false; });
    status(msg, e.message, 'err');
  }
}

async function sendComment() {
  if (!current?.value) return;
  const text = $('rateComment').value.trim(), msg = $('rateMsg');
  if (!text) { hide(); return; }
  const btn = $('rateForm').querySelector('button');
  btn.disabled = true;
  try {
    // the same visitor, scenario and day: the server replaces the vote with this one, comment included
    await post('/ratings', { visitor_id: visitorId(), scenario_id: current.key, case_id: null, value: current.value, comment: text.slice(0, 500) });
    $('rateForm').hidden = true;
    status(msg, 'Спасибо, прочитаем.', 'ok');
    const done = current;
    setTimeout(() => { if (current === done) hide(); }, 2500);
  } catch (e) {
    status(msg, e.message, 'err');
  } finally { btn.disabled = false; }
}
