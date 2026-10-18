// A/B variants from POST /api/v1/assign (docs/contracts/events.md, "Experiments"). The server keeps a visitor's
// variant forever; the answer is also cached in localStorage so a returning visitor gets it without waiting.
// Nothing waits on this: variant(key) is 'a' until an answer is there, and the first read of a key pins it for
// the rest of the page, so the page never switches variant under the viewer's eyes.
import { local, online, post, visitorId } from './api.js';

export const EXPERIMENTS = ['t1_positioning', 't2_onboarding', 't3_share_prompt', 't4_price'];
const CACHE = 'mq_exp';

let assigned = {};
try { const c = JSON.parse(local.get(CACHE) || '{}'); if (c && typeof c === 'object') assigned = c; } catch { /* a broken cache */ }
const pinned = {};
const overrides = {}; // ?debug&exp=t4_price:b,t2_onboarding:b for checking a variant by hand

export function variant(key) {
  if (!(key in pinned)) pinned[key] = overrides[key] || (typeof assigned[key] === 'string' ? assigned[key] : 'a');
  return pinned[key];
}
export const assignments = () => ({ ...assigned });

let done;
export const answered = new Promise(r => { done = r; });

export function start() {
  const p = new URLSearchParams(location.search);
  if (p.has('debug')) for (const kv of (p.get('exp') || '').split(',')) { const [k, v] = kv.split(':'); if (k && /^[a-z]$/.test(v || '')) overrides[k] = v; }
  online.then(async ok => {
    if (!ok) return done(false);
    const utm = {};
    for (const k of ['source', 'medium', 'campaign', 'content', 'term']) { const v = p.get('utm_' + k); if (v) utm[k] = v.slice(0, 200); }
    try {
      const r = await post('/assign', { visitor_id: visitorId(), experiments: EXPERIMENTS, referrer: document.referrer || null, ...(Object.keys(utm).length ? { utm } : {}) });
      // inactive experiments are left out of the answer: they fall back to 'a'
      assigned = r && typeof r.assignments === 'object' ? r.assignments : {};
      local.set(CACHE, JSON.stringify(assigned));
      done(true);
    } catch { done(false); }
  });
}

// resolves when the variants are known or after ms, whichever comes first; a cached answer counts as known
export function settle(ms) {
  if (Object.keys(assigned).length) return Promise.resolve(true);
  return Promise.race([answered, new Promise(r => setTimeout(() => r(false), ms))]);
}
