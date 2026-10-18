// "Скачать клип": up to 20 s of the scene recorded in the browser, with the site's address as a small watermark in
// the corner. Every frame of sim.canvas is drawn onto a 2D canvas together with the watermark, and that canvas is
// recorded with MediaRecorder: MP4 where the browser can, WebM otherwise. Works without the server. Event clip_export.
//
// The WebGL canvas is not created with preserveDrawingBuffer, so its picture can only be copied in the same
// animation frame the simulator drew it in, before the browser presents it. Our requestAnimationFrame callback is
// queued after the simulator's (it asks for its next frame first), so it runs right after the render.
import { sim } from '../bus.js';
import { track } from './analytics.js';
import { $, status } from './ui.js';

export const MAX_SECONDS = 20;
const MAX_W = 1280, MAX_H = 720; // the free clip is 720p at most; HD without the watermark is the Pro door
const TYPES = ['video/mp4;codecs=avc1.42E01E', 'video/mp4;codecs=avc1', 'video/mp4', 'video/webm;codecs=vp9', 'video/webm;codecs=vp8', 'video/webm'];

export const supported = () => typeof MediaRecorder === 'function' && typeof HTMLCanvasElement.prototype.captureStream === 'function'
  && TYPES.some(t => MediaRecorder.isTypeSupported(t));
export const mimeType = () => TYPES.find(t => MediaRecorder.isTypeSupported(t));
export const watermarkText = () => (location.host ? location.host + ' · ' : '') + 'Баллистический манекен';

// the clip's size: the scene's aspect, at most 1280 × 720, even sides (H.264 needs them)
export function clipSize(w, h) {
  const k = Math.min(1, MAX_W / w, MAX_H / h);
  return [Math.max(2, Math.round(w * k / 2) * 2), Math.max(2, Math.round(h * k / 2) * 2)];
}

// one frame: the scene scaled to the clip, the watermark in the bottom-right corner
export function composeFrame(ctx, src, mark = watermarkText()) {
  const { width: w, height: h } = ctx.canvas;
  ctx.drawImage(src, 0, 0, w, h);
  const size = Math.max(11, Math.round(h * 0.026)), pad = Math.round(size * 0.8);
  ctx.save();
  ctx.font = `600 ${size}px Barlow, "Segoe UI", Roboto, sans-serif`;
  ctx.textAlign = 'right'; ctx.textBaseline = 'alphabetic';
  const tw = ctx.measureText(mark).width;
  ctx.fillStyle = 'rgba(15, 20, 23, 0.55)';
  ctx.beginPath();
  ctx.roundRect(w - pad - tw - size * 0.6, h - pad - size * 1.15, tw + size * 1.2, size * 1.6, size * 0.3);
  ctx.fill();
  ctx.fillStyle = 'rgba(255, 255, 255, 0.92)';
  ctx.fillText(mark, w - pad, h - pad);
  ctx.restore();
}

// a composed frame, copied right after the simulator's next render (for checks and the share preview)
export function snapshot() {
  return new Promise(resolve => requestAnimationFrame(() => {
    const [w, h] = clipSize(sim.canvas.width, sim.canvas.height);
    const out = Object.assign(document.createElement('canvas'), { width: w, height: h });
    composeFrame(out.getContext('2d'), sim.canvas);
    resolve(out);
  }));
}

let rec = null; // the recording running: { mr, started, timer, raf }
export let last = null; // the last clip: { size, type, seconds } (for tests)

export function init() {
  const btn = $('clipBtn');
  if (!supported()) { btn.hidden = true; return; }
  btn.addEventListener('click', () => (rec ? stop() : startRecording()));
}

function label() {
  const btn = $('clipBtn');
  if (!rec) { btn.textContent = 'Скачать клип'; btn.setAttribute('aria-pressed', 'false'); return; }
  const s = Math.min(MAX_SECONDS, Math.floor((performance.now() - rec.started) / 1000));
  btn.textContent = `Остановить · ${s} с из ${MAX_SECONDS}`;
  btn.setAttribute('aria-pressed', 'true');
}

export function startRecording() {
  if (rec || !sim.canvas) return;
  const note = $('shareNote');
  const [w, h] = clipSize(sim.canvas.width, sim.canvas.height);
  const out = Object.assign(document.createElement('canvas'), { width: w, height: h });
  const ctx = out.getContext('2d');
  const type = mimeType();
  let mr;
  try {
    mr = new MediaRecorder(out.captureStream(30), { mimeType: type, videoBitsPerSecond: 5_000_000 });
  } catch {
    status(note, 'Этот браузер не умеет записывать видео со страницы.', 'err');
    return;
  }
  const chunks = [];
  mr.ondataavailable = e => { if (e.data?.size) chunks.push(e.data); };
  mr.onstop = () => {
    const seconds = Math.round(Math.min(MAX_SECONDS, (performance.now() - started) / 1000) * 10) / 10;
    const blob = new Blob(chunks, { type: type.split(';')[0] });
    const format = type.startsWith('video/mp4') ? 'mp4' : 'webm';
    last = { size: blob.size, type: blob.type, seconds };
    if (!blob.size) { status(note, 'Клип не записался: браузер не отдал ни одного кадра.', 'err'); return; }
    download(blob, `manekin-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.${format}`);
    track('clip_export', { seconds, format });
    status(note, `Клип ${seconds.toLocaleString('ru')} с сохранён (${format.toUpperCase()}, ${(blob.size / 1e6).toLocaleString('ru', { maximumFractionDigits: 1 })} МБ).`, 'ok');
  };
  const draw = () => {
    if (!rec) return;
    composeFrame(ctx, sim.canvas);
    rec.raf = requestAnimationFrame(draw);
  };
  const started = performance.now();
  rec = { mr, started, raf: 0, timer: setInterval(() => { label(); if (performance.now() - started >= MAX_SECONDS * 1000) stop(); }, 250) };
  rec.raf = requestAnimationFrame(draw);
  mr.start(1000);
  label();
  status(note, `Идёт запись, до ${MAX_SECONDS} с. Нажмите ещё раз, чтобы остановить и скачать.`);
}

export function stop() {
  if (!rec) return;
  const r = rec;
  rec = null;
  clearInterval(r.timer);
  cancelAnimationFrame(r.raf);
  if (r.mr.state !== 'inactive') r.mr.stop();
  label();
}

function download(blob, name) {
  const url = URL.createObjectURL(blob);
  const a = Object.assign(document.createElement('a'), { href: url, download: name });
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60000);
}
