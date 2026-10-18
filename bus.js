// What the simulator announces, for the features built around it (sharing, analytics, experiments).
// They subscribe here instead of reaching into the simulation.
//   ready                                     the scene is built and running
//   shot      { weapon, hit, kind, zones, organs, nearMissCm }   after every shot, once its reaction is chosen
//   reaction  { id, source, title }           a behaviour scenario starts (source: lib | proc | ai)
//   reset                                     the mannequin is set up again
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
