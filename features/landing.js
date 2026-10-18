// The intro over the scene on a first visit: the T1 positioning test (t1_positioning) in place of a separate
// landing page. Not shown for ?case= links or to returning visitors. The card is there at once; its copy appears
// when the variant is known, or after 600 ms with the default (a). The scene keeps loading underneath.
import { local, returning } from './api.js';
import { settle, variant } from './experiments.js';
import { track } from './analytics.js';
import { $, shield } from './ui.js';

export const COPY = {
  a: {
    who: 'Для актёров и режиссёров',
    title: 'Как тело реагирует на ранение',
    sub: 'Попадание почти никогда не отбрасывает человека назад, как в кино. Здесь видно, что бывает на самом деле: вздрагивание, рука к ране, потеря опоры, падение через несколько секунд — или без падения вовсе. Сцену можно замедлить, повторить и посмотреть с любого ракурса.',
  },
  b: {
    who: 'Для аниматоров и геймдева',
    title: 'Реакции на попадание с настоящей физикой',
    sub: 'Active ragdoll на Rapier: тело держит позу «мышцами», а реакцию задаёт задетый орган, а не сила удара. Крутите камеру, замедляйте время до 0,25× и сохраняйте клип как референс для анимации.',
  },
  c: {
    who: 'Для писателей и сценаристов',
    title: 'Что на самом деле происходит после выстрела',
    sub: 'Сколько секунд человек остаётся в сознании, может ли он идти, сколько крови теряет и как это выглядит со стороны. Модель считает раневой канал и кровопотерю по классам ATLS, чтобы сцена в тексте не расходилась с жизнью.',
  },
};

export function shouldShow({ caseId }) {
  const p = new URLSearchParams(location.search);
  if (p.has('debug') && COPY[p.get('intro')]) return true; // a variant previewed by hand
  return !caseId && !returning && !local.get('mq_intro');
}

// shows the intro; resolves when the viewer closes it
export function show() {
  const box = $('intro'), card = $('introCopy');
  const preview = new URLSearchParams(location.search).has('debug') ? new URLSearchParams(location.search).get('intro') : null;
  box.hidden = false;
  shield(box);
  document.body.classList.add('intro-open');
  $('app').inert = true; // the scene and panel behind are out of reach until the intro is closed
  let shown = 'a';
  const fill = () => {
    shown = COPY[preview] ? preview : variant('t1_positioning');
    const c = COPY[shown] || COPY.a;
    $('introWho').textContent = c.who;
    $('introTitle').textContent = c.title;
    $('introSub').textContent = c.sub;
    box.dataset.variant = shown;
    card.classList.remove('pending');
  };
  settle(600).then(fill);
  $('introCta').focus({ preventScroll: true });
  return new Promise(resolve => {
    const close = cta => {
      if (box.hidden) return;
      if (card.classList.contains('pending')) fill();
      if (cta) track('landing_cta', { variant: shown });
      local.set('mq_intro', '1');
      box.hidden = true;
      document.body.classList.remove('intro-open');
      $('app').inert = false;
      removeEventListener('keydown', esc, true);
      resolve();
    };
    const esc = e => { if (e.key === 'Escape') { e.preventDefault(); close(false); } };
    addEventListener('keydown', esc, true);
    $('introCta').addEventListener('click', () => close(true), { once: true });
  });
}
