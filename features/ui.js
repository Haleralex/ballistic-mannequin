// Small DOM helpers shared by the features: a notice over the scene, modal dialogs, the clipboard.
export const $ = id => document.getElementById(id);

// The simulator's keyboard shortcuts listen on the window (Space pauses and swallows the key, R resets). Keys
// pressed on the features' own controls stay with them, the way the simulator already leaves inputs alone.
export function shield(node) {
  if (node._shielded) return;
  node._shielded = true;
  node.addEventListener('keydown', e => { if (e.key !== 'Escape' && e.target.closest?.('button, input, textarea, select, a')) e.stopPropagation(); });
}

// a short notice at the top of the scene; kind: '' | 'err' | 'ok'
let toastTimer = 0;
export function toast(text, { kind = '', ms = 5000 } = {}) {
  const t = $('toast');
  if (!t) return;
  t.textContent = text;
  t.dataset.kind = kind;
  t.hidden = !text;
  clearTimeout(toastTimer);
  if (text && ms) toastTimer = setTimeout(() => { t.hidden = true; }, ms);
}

// A native <dialog>: modal, Escape closes it, the page behind is inert. Focus goes to the first field (or the
// element marked autofocus) and comes back to the button that opened it.
export function openModal(dlg, opener = document.activeElement) {
  if (dlg.open) return;
  dlg.showModal();
  const first = dlg.querySelector('[autofocus]') || dlg.querySelector('input, textarea, button:not(.modal-x)');
  first?.focus();
  dlg.addEventListener('close', () => { if (opener?.isConnected) opener.focus(); }, { once: true });
}
// clicks on the backdrop (outside the dialog's box) close it too
export function closeOnBackdrop(dlg) {
  dlg.addEventListener('click', e => {
    if (e.target !== dlg) return;
    const r = dlg.getBoundingClientRect();
    if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) dlg.close();
  });
}

// copy text; the Clipboard API needs a secure context and permission, the old way needs a selected field
export async function copyText(text, field) {
  try { await navigator.clipboard.writeText(text); return true; } catch { /* fall back */ }
  try {
    if (!field) return false;
    field.focus(); field.select();
    return document.execCommand('copy');
  } catch { return false; }
}

// set a status line: text, and kind '' | 'err' | 'ok'
export function status(node, text, kind = '') {
  node.textContent = text;
  node.dataset.kind = kind;
}
