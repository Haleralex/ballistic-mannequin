// What the simulator announces, for the features built around it (sharing, analytics, experiments).
// They subscribe here instead of reaching into the simulation.
//   ready                                     the scene is built and running
//   shot      { weapon, hit, kind, zones, organs, nearMissCm }   after every shot, once its reaction is chosen
//   reaction  { id, source, title }           a behaviour scenario starts (source: lib | proc | ai)
//   reaction-watched { id, seconds }          a scenario has run 10 s of simulated time (once per scenario)
//   reset                                     the mannequin is set up again
//   case-loaded { case }                      a case was loaded for replay
//   replay-end                                the replay has reached case.steps
// Shots and reactions of a case playing back are announced too: sim.replaying tells them apart from the viewer's.
const handlers = new Map();
export function on(name, fn) {
  if (!handlers.has(name)) handlers.set(name, new Set());
  handlers.get(name).add(fn);
  return () => handlers.get(name).delete(fn);
}
export function emit(name, data) {
  for (const fn of handlers.get(name) || []) {
    try { fn(data); } catch (e) { console.error(`bus: handler for "${name}" failed`, e); } // a broken listener must not stop the simulation
  }
}

// The simulator's API for those features, filled in by app.js (docs/contracts/sim-api.md):
//   getCase() loadCase(c) takeOver() seek(step) stateHash() setBlood(on), and step, replaying, canvas.
// A replay that reaches its end holds the last step (sim.replaying stays true) until takeOver(), seek() or a reset.
export const sim = {};
