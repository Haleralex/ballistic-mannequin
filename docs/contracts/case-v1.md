# Case v1 — a reproducible reaction

A **case** is everything needed to replay one session of the simulator exactly: the seed of the random numbers and the
inputs in the order and at the physics step they happened. It is not a recording of the motion; the simulator computes
the motion again. Owner of the format: team A (determinism). Consumers: team B (stores it), team C (share, replay page).

## Time

- Physics runs at a fixed 120 steps per second (`DT = 1/120`). **Step 0 is the first step after a reset** (`buildWorld`).
- Every input is stamped with the step **before which** it applies: an event with `step: 214` is applied after 214 steps
  have run and before step 215.
- Real time, frame rate, the time scale buttons and pauses do not exist in a case. Only steps.

## Format

```json
{
  "v": 1,
  "seed": 3141592653,
  "build": "f2f18ed",
  "steps": 7800,
  "events": [
    { "step": 0,   "type": "setting", "key": "react",   "value": "flee" },
    { "step": 0,   "type": "setting", "key": "weapon",  "value": "pistol" },
    { "step": 214, "type": "shot", "origin": [0.9, 1.45, 4.2], "dir": [-0.19, -0.09, -0.98] },
    { "step": 216, "type": "script", "source": "ai", "title": "…", "beats": [ { "t": 0.2, "dur": 2, "action": "look", "at": "wound", "intensity": 0.8, "note": "…" } ] }
  ],
  "check": [ { "step": 1200, "hash": "9f3a0c1e" }, { "step": 2400, "hash": "…" } ],
  "meta": { "title": "Печень, пистолет", "weapon": "pistol", "kind": "abdomen", "zones": ["живот"], "organs": ["печень"] }
}
```

| Field | Meaning |
|-------|---------|
| `v` | Format version, always `1`. |
| `seed` | Unsigned 32-bit integer seeding every random choice that affects the simulation (pellet spread, scenario choice, jitter in reactions, flee direction). |
| `build` | Version of the simulator that made the case (short git sha). A replay on a different build may differ; the player shows a notice and plays anyway. |
| `steps` | How long the case runs, in steps (at most 120 × 180 = 21 600). |
| `events` | Inputs, sorted by `step`, stable order within a step. At most 200. |
| `check` | Optional state hashes every 1200 steps (10 s) for verifying a replay. |
| `meta` | Description for search, titles and link previews. Never read by the simulation. |

### Event types

| `type` | Fields | Applied as |
|--------|--------|------------|
| `setting` | `key`, `value` | `weapon`: `pistol` · `rifle` · `buck` · `slug`. `impulse`: `real` · `movie`. `react`: `auto` · `flee` · `stay`. `bleed`: `1` · `10` (the blood-loss speed-up). |
| `shot` | `origin` [x,y,z], `dir` [x,y,z] (unit vector), world metres | A shot along that ray with the current weapon. Pellets of the buckshot spread from the seeded generator. Camera-independent: the replay may view from anywhere. |
| `script` | `source`, `title`, `beats` (same shape as the scenario timeline), `temperament?` | Replaces the scenario chosen for the preceding shot. Used for scenarios composed by Claude, which cannot be recomputed. Library and procedural scenarios are recomputed from the seed and are **not** recorded. |

## Determinism rules (for team A)

1. Every random draw that can change bodies, scenarios or physiology comes from one seeded generator (mulberry32),
   created at reset from `seed`. Purely visual randomness (dust, blood droplets, tracer flicker) uses `Math.random` and
   must not feed back into the simulation.
2. Nothing in the simulation reads real time (`performance.now`, frame `dt`). The blood-loss speed-up is a setting event.
3. Shots arrive as rays, never as screen coordinates.
4. The **state hash** is FNV-1a over the positions and rotations of the 14 bodies rounded to 1e-4, plus
   `character.state`, `character.lost` rounded to 0.1 ml, and the ids of running beats. Hex, 8 characters.
5. Guarantee: the same case on the same build in Chromium gives identical hashes at every check. Across browsers
   (SpiderMonkey's `Math.sin` differs from V8's) a drift is possible and is measured, not promised.

## Limits

JSON at most 64 KB. The server rejects anything larger or failing the shape above with `422`.
