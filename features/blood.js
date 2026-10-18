// The "Без крови" switch: sim.setBlood, remembered in localStorage (mq_noblood), event blood_toggle { on } where
// `on` is the state of the switch itself (true: blood hidden). The intro carries the same switch.
import { on as onBus, sim } from '../bus.js';
import { local } from './api.js';
import { track } from './analytics.js';
import { $ } from './ui.js';

let noBlood = local.get('mq_noblood') === '1';
const boxes = () => [$('noBlood'), $('introNoBlood')].filter(Boolean);

function apply() {
  for (const b of boxes()) b.checked = noBlood;
  if (sim.setBlood) sim.setBlood(!noBlood);
}

export function init() {
  for (const b of boxes()) b.addEventListener('change', () => {
    noBlood = b.checked;
    local.set('mq_noblood', noBlood ? '1' : null);
    apply();
    track('blood_toggle', { on: noBlood });
  });
  for (const b of boxes()) b.checked = noBlood;
  if (sim.setBlood) apply(); else onBus('ready', apply);
}
