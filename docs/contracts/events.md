# Analytics events and experiments

Owner: team C sends them, team B stores and analyses them. Names are `snake_case`, English; props as listed.
The server rejects names not in this list.

## Events

| Event | Props | When |
|-------|-------|------|
| `visit` | `page` (`landing` · `sim` · `replay`), `referrer`, `utm` | Page load. |
| `landing_cta` | `variant` | "Попробовать" on the landing. |
| `sim_ready` | `load_ms`, `gpu`, `software_gl` | `ready` from the bus. |
| `shot` | `weapon`, `hit`, `kind`, `zones`, `near_miss_cm` | `shot` from the bus. |
| `reaction_watched` | `scenario_id`, `source` | `reaction-watched` from the bus (a reaction ran 10 s). |
| `share_link` | `case_id`, `kind` | A share link was created. |
| `share_open` | `case_id` | A shared link was opened. |
| `share_convert` | `case_id` | After opening a shared link, the visitor fired their own shot. |
| `clip_export` | `seconds`, `format` | A clip was downloaded. |
| `rate` | `scenario_id`, `value` | A realism rating. |
| `fake_door_open` | `door` (`room` · `pro`) | A not-yet-built feature was opened. |
| `fake_door_click` | `door`, `price_variant` | "Оформить" on the Pro door. |
| `waitlist_submit` | `door` | An email was left. |
| `blood_toggle` | `on` | The "без крови" switch. |

Return visits (`return_d1`, `return_d7`) are computed in SQL from `visit` events, not sent.

## Derived metrics (SQL views, team B)

- **activated** — a session with ≥ 3 `shot` and ≥ 1 `reaction_watched`.
- **shared** — a session with `share_link` or `clip_export`.
- **share conversion** — `share_convert` / `share_open` per case.
- **waitlist rate** — visitors with `waitlist_submit` / activated visitors.
- Per experiment the unit is the **visitor**: a visitor counts as converted if any of their sessions converted.

## Experiments (seeded by a migration)

| Key | Variants (weights) | Active | Primary metric |
|-----|--------------------|--------|----------------|
| `t1_positioning` | `a` actors and directors, `b` animators and game developers, `c` writers (1 : 1 : 1) | yes | activated |
| `t2_onboarding` | `a` empty sandbox, `b` hint and a guided first shot (1 : 1) | yes | activated |
| `t3_share_prompt` | `a` buttons only, `b` prompt after a reaction (1 : 1) | no (after T2) | shared |
| `t4_price` | `a` 299 ₽/мес, `b` 990 ₽/мес (1 : 1) | no (after T1) | `fake_door_click` |
