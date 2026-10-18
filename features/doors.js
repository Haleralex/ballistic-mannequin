// Fake doors: features that do not exist yet, described honestly as coming, with a waitlist (POST /api/v1/waitlist,
// 152-ФЗ consent required). "Репетиционная комната" (source room) and "Pro" with a price from t4_price
// (a 299 ₽/мес, b 990 ₽/мес; a by default), whose "Оформить" only leads to the waitlist: nothing is charged and no
// card details are asked for. Events: fake_door_open, fake_door_click, waitlist_submit.
import { post, visitorId } from './api.js';
import { track } from './analytics.js';
import { variant } from './experiments.js';
import { $, openModal, closeOnBackdrop, status, shield } from './ui.js';

export const PRICES = { a: '299 ₽/мес', b: '990 ₽/мес' };

export function init() {
  for (const [door, dlgId] of [['room', 'roomDlg'], ['pro', 'proDlg']]) {
    const dlg = $(dlgId);
    shield(dlg);
    closeOnBackdrop(dlg);
    dlg.querySelectorAll('.modal-x, [data-close]').forEach(b => b.addEventListener('click', () => dlg.close()));
    dlg.querySelector('form.wl').addEventListener('submit', e => { e.preventDefault(); submit(door, dlg, e.target); });
    $(door === 'room' ? 'doorRoom' : 'doorPro').addEventListener('click', e => open(door, dlg, e.currentTarget));
  }
  $('proBuy').addEventListener('click', () => {
    const pv = priceVariant();
    track('fake_door_click', { door: 'pro', price_variant: pv });
    $('proOffer').hidden = true;
    $('proWait').hidden = false;
    $('proWait').querySelector('input[type=email]').focus();
  });
}

const priceVariant = () => (PRICES[variant('t4_price')] ? variant('t4_price') : 'a');

function open(door, dlg, opener) {
  const form = dlg.querySelector('form.wl');
  form.hidden = false;
  form.querySelector('button[type=submit]').disabled = false;
  status(form.querySelector('.status'), '');
  dlg.querySelector('.wl-done').hidden = true;
  if (door === 'pro') {
    $('proPrice').textContent = PRICES[priceVariant()];
    $('proOffer').hidden = false;
    $('proWait').hidden = true;
  }
  track('fake_door_open', { door });
  openModal(dlg, opener);
  if (door === 'pro') $('proBuy').focus();
}

async function submit(door, dlg, form) {
  const email = form.querySelector('input[type=email]'), consent = form.querySelector('input[type=checkbox]');
  const msg = form.querySelector('.status'), btn = form.querySelector('button[type=submit]');
  if (!email.value.trim() || !email.checkValidity()) { status(msg, 'Похоже, в адресе почты ошибка.', 'err'); email.focus(); return; }
  if (!consent.checked) { status(msg, 'Без согласия на обработку почты мы не можем её сохранить.', 'err'); consent.focus(); return; }
  btn.disabled = true;
  status(msg, 'Отправляю…');
  const body = { visitor_id: visitorId(), email: email.value.trim(), source: door, consent: true };
  if (door === 'pro') body.price_variant = priceVariant();
  try {
    await post('/waitlist', body);
    track('waitlist_submit', { door });
    form.hidden = true;
    const done = dlg.querySelector('.wl-done');
    done.hidden = false;
    done.querySelector('button').focus();
    email.value = ''; consent.checked = false;
  } catch (e) {
    status(msg, e.message, 'err');
  } finally { btn.disabled = false; }
}
