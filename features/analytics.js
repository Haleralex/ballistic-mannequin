// Analytics events (docs/contracts/events.md): queued, sent in batches every 5 s and on pagehide with sendBeacon.
// Nothing leaves the page when the API is absent. Activity of a case playing back (a shared link, a seek) is the
// simulator's, not the viewer's: shots and reactions announced while sim.replaying are never sent as theirs.
import { on, sim } from '../bus.js';
import { online, visitorId, sessionId } from './api.js';

const BATCH = 100, EVERY = 5000, MAX_QUEUE = 500;
const queue = [];
let enabled = null; // null: not known yet (events wait), then true or false

export function track(name, props = {}) {
  if (enabled === false) return;
  queue.push({ sid: sessionId(), e: { name, at: new Date().toISOString(), props } });
  if (queue.length > MAX_QUEUE) queue.splice(0, queue.length - MAX_QUEUE);
  if (enabled && queue.length >= BATCH) flush();
}

// send what is queued: one request per session id and per 100 events
export function flush(beacon = false) {
  if (!enabled) return;
  while (queue.length) {
    const s = queue[0].sid;
    let n = 0;
    while (n < queue.length && n < BATCH && queue[n].sid === s) n++;
    const part = queue.splice(0, n);
    const body = JSON.stringify({ visitor_id: visitorId(), session_id: s, events: part.map(x => x.e) });
    if (beacon && navigator.sendBeacon) {
      try { if (navigator.sendBeacon('/api/v1/events', body)) continue; } catch { /* fall through to fetch */ }
    }
    fetch('/api/v1/events', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body, keepalive: true })
      .then(r => { if (r.status >= 500) requeue(part); })
      .catch(() => requeue(part));
  }
}
const retried = new WeakSet();
function requeue(part) { // one more try for a batch lost to the network, never more
  const again = part.filter(x => !retried.has(x));
  again.forEach(x => retried.add(x));
  queue.unshift(...again);
}

// the scenario running and whether the viewer's own shot started it (a replayed one never is)
export const reaction = { id: null, source: null, own: false, key: null };
export const scenarioKey = (id, source) => id || (source === 'ai' ? 'ai' : 'proc');

export function start({ page }) {
  const params = new URLSearchParams(location.search), utm = {};
  for (const k of ['source', 'medium', 'campaign', 'content', 'term']) { const v = params.get('utm_' + k); if (v) utm[k] = v.slice(0, 200); }
  track('visit', { page, referrer: document.referrer || null, utm: Object.keys(utm).length ? utm : null });

  online.then(ok => {
    enabled = ok;
    if (!ok) { queue.length = 0; return; }
    flush();
    setInterval(flush, EVERY);
    addEventListener('pagehide', () => flush(true));
    addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') flush(true); });
  });
  // pointer and keys keep the session alive between events
  let touched = 0;
  const touch = () => { const t = Date.now(); if (t - touched > 60000) { touched = t; sessionId(); } };
  addEventListener('pointerdown', touch, { passive: true });
  addEventListener('keydown', touch, { passive: true });

  const ready = () => track('sim_ready', { load_ms: Math.round(performance.now()), ...gpuInfo() });
  if (sim.getCase) ready(); else on('ready', ready);
  on('shot', d => {
    if (sim.replaying) return;
    track('shot', { weapon: d.weapon, hit: d.hit, kind: d.kind, zones: d.zones, near_miss_cm: d.nearMissCm ?? null });
  });
  on('reaction', d => Object.assign(reaction, { id: d.id || null, source: d.source, own: !sim.replaying, key: scenarioKey(d.id, d.source) }));
  on('reaction-watched', () => {
    if (sim.replaying || !reaction.own) return;
    track('reaction_watched', { scenario_id: reaction.key, source: reaction.source });
  });
}

// what the scene draws with: the WebGL context of the simulator's canvas is already there, getContext returns it
function gpuInfo() {
  try {
    const gl = sim.canvas?.getContext('webgl2') || sim.canvas?.getContext('webgl');
    const ext = gl?.getExtension('WEBGL_debug_renderer_info');
    const gpu = gl ? String(gl.getParameter(ext ? ext.UNMASKED_RENDERER_WEBGL : gl.RENDERER)).slice(0, 200) : '';
    return { gpu, software_gl: /swiftshader|llvmpipe|softpipe|software|basic render/i.test(gpu) };
  } catch { return { gpu: '', software_gl: null }; }
}
