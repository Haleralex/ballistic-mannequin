-- 002: the experiments of docs/contracts/events.md. T1 and T2 run first; T3 starts after T2 and T4 after T1,
-- by setting active = true (an UPDATE, or a later migration). Existing rows are left alone so a manual change
-- of active or weights survives.
INSERT INTO experiments (key, variants, weights, metric, active) VALUES
  ('t1_positioning',  ARRAY['a', 'b', 'c'], ARRAY[1, 1, 1], 'activated',       true),
  ('t2_onboarding',   ARRAY['a', 'b'],      ARRAY[1, 1],    'activated',       true),
  ('t3_share_prompt', ARRAY['a', 'b'],      ARRAY[1, 1],    'shared',          false),
  ('t4_price',        ARRAY['a', 'b'],      ARRAY[1, 1],    'fake_door_click', false)
ON CONFLICT (key) DO NOTHING;
