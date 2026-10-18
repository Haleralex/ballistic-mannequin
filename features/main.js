// The growth features around the simulator (docs/contracts/layout.md, team C). Loaded as an async module so the
// intro does not wait for three.js and the physics: everything that needs the scene waits for `ready` on the bus.
// Without the API server (GitHub Pages, a local file) only the offline parts run: the intro, the clip, the
// "без крови" switch; the rest stays hidden.
import { on, sim } from '../bus.js';
import { online } from './api.js';
import * as analytics from './analytics.js';
import * as experiments from './experiments.js';
import * as landing from './landing.js';
import * as onboarding from './onboarding.js';
import * as share from './share.js';
import * as clip from './clip.js';
import * as rating from './rating.js';
import * as doors from './doors.js';
import * as blood from './blood.js';
import { variant, settle } from './experiments.js';

const params = new URLSearchParams(location.search);
const caseId = params.has('case') ? params.get('case') : null;
const intro = landing.shouldShow({ caseId });

experiments.start();
analytics.start({ page: caseId != null ? 'replay' : intro ? 'landing' : 'sim' });
const ready = new Promise(r => { if (sim.getCase) r(); else on('ready', r); });

// notices at the top of the scene go under the state chips, which wrap onto two rows on a phone
const chips = document.getElementById('chips'), stage = document.getElementById('stage');
new ResizeObserver(() => stage.style.setProperty('--chips-b', chips.offsetTop + chips.offsetHeight + 'px')).observe(chips);

blood.init();
share.init({ caseId });
clip.init();
rating.init();
doors.init();

(async () => {
  if (intro) await landing.show();
  if (caseId != null) return; // a shared case is its own introduction
  await settle(600);
  if (variant('t2_onboarding') !== 'b') return;
  await ready;
  if (!sim.replaying) onboarding.start();
})();

if (params.has('debug')) window.__growth = { analytics, experiments, clip, share, online, flush: () => analytics.flush() };
