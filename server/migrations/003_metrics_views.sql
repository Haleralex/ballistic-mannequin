-- 003: derived metrics of docs/contracts/events.md as views, plus the indexes they and the API need.
-- The unit of every experiment is the visitor: a visitor converted if any of their sessions converted.

CREATE INDEX events_visitor_session ON events (visitor_id, session_id);
CREATE INDEX assignments_experiment ON assignments (experiment_key, variant);
CREATE INDEX ratings_scenario ON ratings (scenario_id, day);

-- One row per session: what happened in it.
CREATE VIEW session_metrics AS
SELECT
  visitor_id,
  session_id,
  min(at)                                            AS started_at,
  max(at)                                            AS ended_at,
  count(*) FILTER (WHERE name = 'shot')              AS shots,
  count(*) FILTER (WHERE name = 'reaction_watched')  AS reactions_watched,
  -- activated: ≥ 3 shots and ≥ 1 reaction watched for 10 s, in the same session
  (count(*) FILTER (WHERE name = 'shot') >= 3
   AND count(*) FILTER (WHERE name = 'reaction_watched') >= 1) AS activated,
  -- shared: a share link or a clip export
  bool_or(name IN ('share_link', 'clip_export'))     AS shared,
  bool_or(name = 'sim_ready')                        AS sim_ready,
  bool_or(name = 'shot')                             AS shot,
  bool_or(name = 'fake_door_open')                   AS fake_door_open,
  bool_or(name = 'fake_door_click')                  AS fake_door_click,
  bool_or(name = 'waitlist_submit')                  AS waitlist_submit
FROM events
GROUP BY visitor_id, session_id;

-- One row per visitor that sent any event: did any of their sessions convert.
CREATE VIEW visitor_metrics AS
SELECT
  visitor_id,
  count(*)                AS sessions,
  min(started_at)         AS first_at,
  max(ended_at)           AS last_at,
  bool_or(sim_ready)       AS sim_ready,
  bool_or(shot)            AS shot,
  bool_or(activated)       AS activated,
  bool_or(shared)          AS shared,
  bool_or(fake_door_open)  AS fake_door_open,
  bool_or(fake_door_click) AS fake_door_click,
  bool_or(waitlist_submit) AS waitlist_submit
FROM session_metrics
GROUP BY visitor_id;

-- Share conversion per case: distinct visitors who opened a shared link and who then fired their own shot.
CREATE VIEW share_conversion AS
SELECT
  props->>'case_id'                                                         AS case_id,
  count(DISTINCT visitor_id) FILTER (WHERE name = 'share_open')             AS opened,
  count(DISTINCT visitor_id) FILTER (WHERE name = 'share_convert')          AS converted,
  count(DISTINCT visitor_id) FILTER (WHERE name = 'share_convert')::float8
    / NULLIF(count(DISTINCT visitor_id) FILTER (WHERE name = 'share_open'), 0) AS rate
FROM events
WHERE name IN ('share_open', 'share_convert') AND props ? 'case_id'
GROUP BY props->>'case_id';

-- Waitlist rate: visitors with waitlist_submit / activated visitors (one row).
-- waitlist_activated_visitors counts only those who were activated as well (H3 is about activated visitors).
CREATE VIEW waitlist_rate AS
SELECT
  count(*) FILTER (WHERE activated)                       AS activated_visitors,
  count(*) FILTER (WHERE waitlist_submit)                 AS waitlist_visitors,
  count(*) FILTER (WHERE waitlist_submit AND activated)   AS waitlist_activated_visitors,
  count(*) FILTER (WHERE waitlist_submit)::float8
    / NULLIF(count(*) FILTER (WHERE activated), 0)        AS rate
FROM visitor_metrics;

-- Return visits from visit events: d1 = a visit 24-48 h after the first one, d7 = 7-8 days after it.
CREATE VIEW visitor_returns AS
WITH visits AS (
  SELECT visitor_id, at FROM events WHERE name = 'visit'
), firsts AS (
  SELECT visitor_id, min(at) AS first_visit FROM visits GROUP BY visitor_id
)
SELECT
  f.visitor_id,
  f.first_visit,
  EXISTS (SELECT 1 FROM visits v WHERE v.visitor_id = f.visitor_id
          AND v.at >= f.first_visit + interval '1 day' AND v.at < f.first_visit + interval '2 days') AS return_d1,
  EXISTS (SELECT 1 FROM visits v WHERE v.visitor_id = f.visitor_id
          AND v.at >= f.first_visit + interval '7 days' AND v.at < f.first_visit + interval '8 days') AS return_d7
FROM firsts f;

-- Funnel per experiment variant (assigned visitors; visitors without events count as not converted).
CREATE VIEW experiment_funnel AS
SELECT
  a.experiment_key,
  a.variant,
  count(*)                                           AS visitors,
  count(*) FILTER (WHERE vm.sim_ready)               AS sim_ready,
  count(*) FILTER (WHERE vm.shot)                    AS shot,
  count(*) FILTER (WHERE vm.activated)               AS activated,
  count(*) FILTER (WHERE vm.shared)                  AS shared,
  count(*) FILTER (WHERE vm.fake_door_click)         AS fake_door_click,
  count(*) FILTER (WHERE vm.waitlist_submit)         AS waitlist_submit
FROM assignments a
LEFT JOIN visitor_metrics vm ON vm.visitor_id = a.visitor_id
GROUP BY a.experiment_key, a.variant;
