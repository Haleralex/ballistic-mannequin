-- Postgres 16 schema, contract between teams. Team B turns it into migrations (server/migrations) and may add
-- indexes or views, but changes to tables and columns go through this file first.

CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE visitors (
  id          uuid PRIMARY KEY,
  first_seen  timestamptz NOT NULL DEFAULT now(),
  referrer    text,
  utm         jsonb
);

CREATE TABLE experiments (
  key       text PRIMARY KEY,
  variants  text[] NOT NULL,
  weights   int[]  NOT NULL CHECK (cardinality(weights) = cardinality(variants)),
  metric    text   NOT NULL,            -- primary metric, see events.md
  active    boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE assignments (
  visitor_id     uuid NOT NULL REFERENCES visitors(id),
  experiment_key text NOT NULL REFERENCES experiments(key),
  variant        text NOT NULL,
  assigned_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (visitor_id, experiment_key)
);

CREATE TABLE events (
  id          bigserial PRIMARY KEY,
  visitor_id  uuid NOT NULL,
  session_id  uuid NOT NULL,
  name        text NOT NULL,
  props       jsonb NOT NULL DEFAULT '{}',
  at          timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_name_at ON events (name, at);
CREATE INDEX events_visitor ON events (visitor_id, at);

CREATE TABLE cases (
  id          text PRIMARY KEY,           -- 8 characters base62
  visitor_id  uuid,
  created_at  timestamptz NOT NULL DEFAULT now(),
  build       text NOT NULL,
  seed        bigint NOT NULL,
  kind        text,                       -- meta.kind, for search
  weapon      text,
  data        jsonb NOT NULL,             -- the whole case v1
  views       int NOT NULL DEFAULT 0
);
CREATE INDEX cases_kind ON cases (kind, created_at DESC);

CREATE TABLE ratings (
  id          bigserial PRIMARY KEY,
  visitor_id  uuid NOT NULL,
  scenario_id text NOT NULL,
  case_id     text REFERENCES cases(id),
  value       smallint NOT NULL CHECK (value IN (-1, 1)),
  comment     text CHECK (length(comment) <= 500),
  day         date NOT NULL DEFAULT current_date,
  at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (visitor_id, scenario_id, day)
);

CREATE TABLE waitlist (
  email         citext PRIMARY KEY,
  visitor_id    uuid,
  source        text NOT NULL,            -- room | pro
  price_variant text,
  consent_at    timestamptz NOT NULL,     -- 152-ФЗ: when consent was given
  created_at    timestamptz NOT NULL DEFAULT now()
);
