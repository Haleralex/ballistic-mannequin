# Simulator ↔ features: the JS contract

Features around the simulator (sharing, analytics, experiments, landing) never reach into `app.js`. They use `bus.js`:
events to listen to, and the `sim` object that `app.js` fills in. Owner: team A fills in `sim`; team C consumes it.

## Events (`on(name, fn)`)

| Event | Payload | When |
|-------|---------|------|
| `ready` | — | The scene is built and running. |
| `shot` | `{ weapon, hit, kind, zones, organs, nearMissCm }` | After every shot, once its reaction is chosen. `kind` is the incident type (`near`, `heart`, `chest`, `leg-bone`, …). |
| `reaction` | `{ id, source, title }` | A behaviour scenario starts. `source`: `lib` · `proc` · `ai`. |
| `reaction-watched` | `{ id, seconds }` | Fired once per scenario when it has run 10 s of simulated time (activation metric). Team A adds it. |
| `reset` | — | The mannequin is set up again. |
| `case-loaded` | `{ case }` | A case was loaded for replay. Team A. |
| `replay-end` | — | The replay has reached `case.steps`. Team A. |

## `sim` (`import { sim } from './bus.js'`)

```js
sim.getCase()          // → case v1 of everything since the last reset (seed, settings, shots, AI scripts, meta)
sim.loadCase(c)        // → Promise: resets with c.seed and replays c.events; resolves when step 0 is ready
                       //   (playback then runs in real time; shots by the viewer are blocked until sim.takeOver())
sim.takeOver()         // the viewer continues from the current state: new inputs are recorded on top of the case
sim.seek(step)         // → Promise: jump to a step by replaying from 0 (fast-forward without rendering)
sim.step               // current step since reset (read-only)
sim.replaying          // true while a loaded case is playing back
sim.stateHash()        // → '9f3a0c1e' (case-v1.md, rule 4)
sim.canvas             // the WebGL canvas, for recording clips
sim.setBlood(on)       // show or hide blood (the "без крови" switch)
```

Rules:
- Every function works with the API server absent. `sim` knows nothing about the network.
- `loadCase` validates `v === 1` and rejects with an `Error` whose `message` is user-facing Russian text.
- While `sim.replaying`, clicks on the canvas do not shoot; orbiting the camera still works.
