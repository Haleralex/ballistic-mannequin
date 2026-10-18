// The Go API on this page's own origin (docs/contracts/api.md). GET /healthz decides once whether it is there:
// without it (GitHub Pages, a file opened locally) the online features stay hidden and the simulator works alone.
// Also the anonymous ids: the visitor (localStorage mq_vid) and the session (sessionStorage mq_sid, renewed after
// 30 min without activity). Storage can throw (private mode, blocked site data), so every access is guarded and
// the ids fall back to memory for this page.

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const SESSION_IDLE = 30 * 60 * 1000;

function storage(kind) {
  const area = () => { try { return window[kind] || null; } catch { return null; } };
  return {
    get(k) { try { return area()?.getItem(k) ?? null; } catch { return null; } },
    set(k, v) { try { if (v == null) area()?.removeItem(k); else area()?.setItem(k, String(v)); } catch { /* storage unavailable */ } },
  };
}
export const local = storage('localStorage');
export const session = storage('sessionStorage');

// UUID v4; crypto.randomUUID exists only in secure contexts, getRandomValues everywhere
export function uuid() {
  try { if (crypto.randomUUID) return crypto.randomUUID(); } catch { /* not a secure context */ }
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40; b[8] = (b[8] & 0x3f) | 0x80;
  const h = [...b].map(x => x.toString(16).padStart(2, '0')).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

// a visitor who has been here before (read before this page creates the id)
export const returning = UUID.test(local.get('mq_vid') || '');
let vid = null;
export function visitorId() {
  if (vid) return vid;
  vid = local.get('mq_vid');
  if (!UUID.test(vid || '')) { vid = uuid(); local.set('mq_vid', vid); }
  return vid;
}

let sid = null, sidAt = 0;
// the session id, renewed after 30 min of inactivity; every call counts as activity
export function sessionId() {
  const now = Date.now();
  if (!sid) { sid = session.get('mq_sid'); sidAt = +session.get('mq_sid_at') || 0; }
  if (!UUID.test(sid || '') || now - sidAt > SESSION_IDLE) sid = uuid();
  sidAt = now;
  session.set('mq_sid', sid); session.set('mq_sid_at', now);
  return sid;
}

// ---------- is the API there ----------
let state = null; // null until known, then true or false
export let build = null;
const html = document.documentElement;
export const online = (async () => {
  if (!/^https?:$/.test(location.protocol)) return false;
  const ctl = new AbortController(), timer = setTimeout(() => ctl.abort(), 4000);
  try {
    const r = await fetch('/healthz', { cache: 'no-store', signal: ctl.signal });
    const j = r.ok ? await r.json() : null;
    if (j?.status !== 'ok') return false;
    build = typeof j.build === 'string' ? j.build : null;
    return true;
  } catch { return false; }
  finally { clearTimeout(timer); }
})().then(ok => {
  state = ok;
  html.classList.toggle('online', ok); // .online-only elements show only now
  return ok;
});
export const isOnline = () => state === true;

// ---------- requests ----------
const NO_LINK = 'Нет связи с сервером. Проверьте интернет и попробуйте ещё раз.';
async function answer(r) {
  let j = null;
  try { j = await r.json(); } catch { /* an empty or non-JSON body */ }
  if (r.ok) return j ?? {};
  const e = new Error(j?.message || (r.status === 429 ? 'Слишком много запросов. Подождите минуту.' : 'Сервер не смог выполнить запрос. Попробуйте позже.'));
  e.status = r.status; e.code = j?.error;
  throw e;
}
export async function post(path, body) {
  let r;
  try { r = await fetch('/api/v1' + path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); }
  catch { throw new Error(NO_LINK); }
  return answer(r);
}
export async function get(path) {
  let r;
  try { r = await fetch('/api/v1' + path, { cache: 'no-store' }); }
  catch { throw new Error(NO_LINK); }
  return answer(r);
}
