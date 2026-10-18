// Package store holds every SQL query of the service.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Haleralex/ballistic-mannequin/server/internal/assign"
	"github.com/Haleralex/ballistic-mannequin/server/internal/taxonomy"
)

var (
	// ErrNotFound means the row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrDuplicate means a unique key (a case id) is taken.
	ErrDuplicate = errors.New("store: duplicate key")
	// ErrUnknownCase means a rating names a case that does not exist.
	ErrUnknownCase = errors.New("store: unknown case")
)

// Store wraps the connection pool.
type Store struct{ Pool *pgxpool.Pool }

// New returns a Store on pool.
func New(pool *pgxpool.Pool) *Store { return &Store{Pool: pool} }

// Ping checks that the database answers.
func (s *Store) Ping(ctx context.Context) error { return s.Pool.Ping(ctx) }

func uuidOf(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	err := u.Scan(s)
	return u, err
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// Visitor is what the browser tells about itself on first contact.
type Visitor struct {
	ID       string
	Referrer *string
	UTM      []byte // a JSON object or nil
}

// Assign upserts the visitor and returns the sticky variant of each requested experiment that exists and is
// active. A new assignment is drawn with assign.Pick; an existing one is returned unchanged.
func (s *Store) Assign(ctx context.Context, v Visitor, keys []string) (map[string]string, error) {
	out := map[string]string{}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	// First contact keeps the first referrer and utm; later contacts only fill them in if they were empty.
	if _, err := tx.Exec(ctx, `
		INSERT INTO visitors (id, referrer, utm) VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET
			referrer = COALESCE(visitors.referrer, EXCLUDED.referrer),
			utm      = COALESCE(visitors.utm, EXCLUDED.utm)`,
		v.ID, v.Referrer, v.UTM); err != nil {
		return nil, fmt.Errorf("store: upsert visitor: %w", err)
	}
	if len(keys) == 0 {
		return out, tx.Commit(ctx)
	}
	rows, err := tx.Query(ctx, `SELECT key, variants, weights FROM experiments WHERE active AND key = ANY($1)`, keys)
	if err != nil {
		return nil, err
	}
	type exp struct {
		key      string
		variants []string
		weights  []int
	}
	var exps []exp
	for rows.Next() {
		var e exp
		var w []int32
		if err := rows.Scan(&e.key, &e.variants, &w); err != nil {
			rows.Close()
			return nil, err
		}
		for _, x := range w {
			e.weights = append(e.weights, int(x))
		}
		exps = append(exps, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(exps) == 0 {
		return out, tx.Commit(ctx)
	}
	activeKeys := make([]string, 0, len(exps))
	b := &pgx.Batch{}
	for _, e := range exps {
		variant := assign.Pick(v.ID, e.key, e.variants, e.weights)
		if variant == "" {
			continue
		}
		activeKeys = append(activeKeys, e.key)
		b.Queue(`INSERT INTO assignments (visitor_id, experiment_key, variant) VALUES ($1, $2, $3)
			ON CONFLICT (visitor_id, experiment_key) DO NOTHING`, v.ID, e.key, variant)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, fmt.Errorf("store: insert assignments: %w", err)
	}
	rows, err = tx.Query(ctx, `SELECT experiment_key, variant FROM assignments WHERE visitor_id = $1 AND experiment_key = ANY($2)`,
		v.ID, activeKeys)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k, variant string
		if err := rows.Scan(&k, &variant); err != nil {
			rows.Close()
			return nil, err
		}
		out[k] = variant
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// Event is one analytics event to store.
type Event struct {
	VisitorID string
	SessionID string
	Name      string
	Props     []byte // a JSON object
	At        time.Time
}

// InsertEvents stores events with COPY.
func (s *Store) InsertEvents(ctx context.Context, evs []Event) error {
	if len(evs) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(evs))
	for _, e := range evs {
		vid, err := uuidOf(e.VisitorID)
		if err != nil {
			return err
		}
		sid, err := uuidOf(e.SessionID)
		if err != nil {
			return err
		}
		rows = append(rows, []any{vid, sid, e.Name, e.Props, e.At})
	}
	_, err := s.Pool.CopyFrom(ctx, pgx.Identifier{"events"}, []string{"visitor_id", "session_id", "name", "props", "at"},
		pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("store: insert events: %w", err)
	}
	return nil
}

// Case is a stored case.
type Case struct {
	ID        string
	VisitorID *string
	Build     string
	Seed      int64
	Kind      *string
	Weapon    *string
	Data      []byte // case v1 JSON
	Views     int
	CreatedAt time.Time
}

// CreateCase inserts a case. It returns ErrDuplicate if the id is taken.
func (s *Store) CreateCase(ctx context.Context, c Case) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO cases (id, visitor_id, build, seed, kind, weapon, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, c.ID, c.VisitorID, c.Build, c.Seed, c.Kind, c.Weapon, c.Data)
	if pgCode(err) == "23505" {
		return ErrDuplicate
	}
	if err != nil {
		return fmt.Errorf("store: create case: %w", err)
	}
	return nil
}

// GetCase reads a case by id.
func (s *Store) GetCase(ctx context.Context, id string) (*Case, error) {
	var c Case
	var vid pgtype.UUID
	err := s.Pool.QueryRow(ctx, `SELECT id, visitor_id, build, seed, kind, weapon, data, views, created_at
		FROM cases WHERE id = $1`, id).Scan(&c.ID, &vid, &c.Build, &c.Seed, &c.Kind, &c.Weapon, &c.Data, &c.Views, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get case: %w", err)
	}
	if vid.Valid {
		s := vid.String()
		c.VisitorID = &s
	}
	return &c, nil
}

// CountView adds one view to a case.
func (s *Store) CountView(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE cases SET views = views + 1 WHERE id = $1`, id)
	return err
}

// Rating is a realism vote.
type Rating struct {
	VisitorID  string
	ScenarioID string
	CaseID     *string
	Value      int
	Comment    *string
	Day        time.Time // the UTC date of the vote
}

// UpsertRating stores a rating; a later vote of the same visitor on the same scenario and day replaces it.
func (s *Store) UpsertRating(ctx context.Context, r Rating) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO ratings (visitor_id, scenario_id, case_id, value, comment, day) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (visitor_id, scenario_id, day) DO UPDATE SET
			case_id = EXCLUDED.case_id, value = EXCLUDED.value, comment = EXCLUDED.comment, at = now()`,
		r.VisitorID, r.ScenarioID, r.CaseID, r.Value, r.Comment, pgtype.Date{Time: r.Day, Valid: true})
	if pgCode(err) == "23503" {
		return ErrUnknownCase
	}
	if err != nil {
		return fmt.Errorf("store: upsert rating: %w", err)
	}
	return nil
}

// WaitlistEntry is an email left on a fake door.
type WaitlistEntry struct {
	Email        string
	VisitorID    *string
	Source       string
	PriceVariant *string
	ConsentAt    time.Time
	CreatedAt    time.Time
}

// AddToWaitlist inserts the address unless it is already there (then nothing changes).
func (s *Store) AddToWaitlist(ctx context.Context, w WaitlistEntry) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO waitlist (email, visitor_id, source, price_variant, consent_at)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT (email) DO NOTHING`,
		w.Email, w.VisitorID, w.Source, w.PriceVariant, w.ConsentAt)
	if err != nil {
		return fmt.Errorf("store: waitlist: %w", err)
	}
	return nil
}

// EachWaitlist calls fn for every waitlist entry, oldest first.
func (s *Store) EachWaitlist(ctx context.Context, fn func(WaitlistEntry) error) error {
	rows, err := s.Pool.Query(ctx, `SELECT email::text, visitor_id, source, price_variant, consent_at, created_at
		FROM waitlist ORDER BY created_at, email`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var w WaitlistEntry
		var vid pgtype.UUID
		if err := rows.Scan(&w.Email, &vid, &w.Source, &w.PriceVariant, &w.ConsentAt, &w.CreatedAt); err != nil {
			return err
		}
		if vid.Valid {
			s := vid.String()
			w.VisitorID = &s
		}
		if err := fn(w); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Experiment is a row of experiments.
type Experiment struct {
	Key      string
	Variants []string
	Weights  []int32
	Metric   string
	Active   bool
}

// Experiments lists all experiments, active first.
func (s *Store) Experiments(ctx context.Context) ([]Experiment, error) {
	rows, err := s.Pool.Query(ctx, `SELECT key, variants, weights, metric, active FROM experiments ORDER BY active DESC, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Experiment
	for rows.Next() {
		var e Experiment
		if err := rows.Scan(&e.Key, &e.Variants, &e.Weights, &e.Metric, &e.Active); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Experiment reads one experiment.
func (s *Store) Experiment(ctx context.Context, key string) (*Experiment, error) {
	var e Experiment
	err := s.Pool.QueryRow(ctx, `SELECT key, variants, weights, metric, active FROM experiments WHERE key = $1`, key).
		Scan(&e.Key, &e.Variants, &e.Weights, &e.Metric, &e.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// VariantCount is the number of assigned visitors of a variant and how many of them converted.
type VariantCount struct {
	Variant   string
	Visitors  int64
	Converted int64
}

// Conversions counts, per variant of the experiment, assigned visitors and those converted on metric.
func (s *Store) Conversions(ctx context.Context, key string, m taxonomy.Metric) (map[string]VariantCount, error) {
	var rows pgx.Rows
	var err error
	if m.Column != "" {
		if _, ok := taxonomy.ResolveMetric(m.Column); !ok { // the column name goes into the SQL text
			return nil, fmt.Errorf("store: unknown metric column %q", m.Column)
		}
		rows, err = s.Pool.Query(ctx, `
			SELECT a.variant, count(*), count(*) FILTER (WHERE vm.`+m.Column+`)
			FROM assignments a LEFT JOIN visitor_metrics vm ON vm.visitor_id = a.visitor_id
			WHERE a.experiment_key = $1 GROUP BY a.variant`, key)
	} else {
		rows, err = s.Pool.Query(ctx, `
			SELECT a.variant, count(*),
			       count(*) FILTER (WHERE EXISTS (SELECT 1 FROM events e WHERE e.visitor_id = a.visitor_id AND e.name = $2))
			FROM assignments a
			WHERE a.experiment_key = $1 GROUP BY a.variant`, key, m.Event)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]VariantCount{}
	for rows.Next() {
		var c VariantCount
		if err := rows.Scan(&c.Variant, &c.Visitors, &c.Converted); err != nil {
			return nil, err
		}
		out[c.Variant] = c
	}
	return out, rows.Err()
}

// FunnelRow is one variant of the experiment_funnel view.
type FunnelRow struct {
	Variant                                                          string
	Visitors, SimReady, Shot, Activated, Shared, DoorClick, Waitlist int64
}

// Funnel reads experiment_funnel for one experiment.
func (s *Store) Funnel(ctx context.Context, key string) (map[string]FunnelRow, error) {
	rows, err := s.Pool.Query(ctx, `SELECT variant, visitors, sim_ready, shot, activated, shared, fake_door_click, waitlist_submit
		FROM experiment_funnel WHERE experiment_key = $1`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FunnelRow{}
	for rows.Next() {
		var f FunnelRow
		if err := rows.Scan(&f.Variant, &f.Visitors, &f.SimReady, &f.Shot, &f.Activated, &f.Shared, &f.DoorClick, &f.Waitlist); err != nil {
			return nil, err
		}
		out[f.Variant] = f
	}
	return out, rows.Err()
}

// Summary is the headline numbers of the admin page.
type Summary struct {
	Visitors          int64    // rows in visitors
	VisitorsWithEvent int64    // visitors that sent any event
	Activated         int64    // activated visitors
	Shared            int64    // visitors who shared
	Waitlist          int64    // waitlist entries
	WaitlistVisitors  int64    // visitors with a waitlist_submit event
	WaitlistRate      *float64 // waitlist_rate.rate
	Cases             int64
	CaseViews         int64
	ShareOpened       int64 // distinct (case, visitor) share opens
	ShareConverted    int64
	RatingsUp         int64
	RatingsDown       int64
	ReturnD1          int64
	ReturnD7          int64
}

// Summary computes the headline numbers.
func (s *Store) Summary(ctx context.Context) (*Summary, error) {
	var m Summary
	err := s.Pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM visitors),
		  (SELECT count(*) FROM visitor_metrics),
		  (SELECT count(*) FROM visitor_metrics WHERE activated),
		  (SELECT count(*) FROM visitor_metrics WHERE shared),
		  (SELECT count(*) FROM waitlist),
		  (SELECT waitlist_visitors FROM waitlist_rate),
		  (SELECT rate FROM waitlist_rate),
		  (SELECT count(*) FROM cases),
		  (SELECT coalesce(sum(views), 0)::bigint FROM cases),
		  (SELECT coalesce(sum(opened), 0)::bigint FROM share_conversion),
		  (SELECT coalesce(sum(converted), 0)::bigint FROM share_conversion),
		  (SELECT count(*) FROM ratings WHERE value = 1),
		  (SELECT count(*) FROM ratings WHERE value = -1),
		  (SELECT count(*) FROM visitor_returns WHERE return_d1),
		  (SELECT count(*) FROM visitor_returns WHERE return_d7)`).
		Scan(&m.Visitors, &m.VisitorsWithEvent, &m.Activated, &m.Shared, &m.Waitlist, &m.WaitlistVisitors, &m.WaitlistRate,
			&m.Cases, &m.CaseViews, &m.ShareOpened, &m.ShareConverted, &m.RatingsUp, &m.RatingsDown, &m.ReturnD1, &m.ReturnD7)
	if err != nil {
		return nil, fmt.Errorf("store: summary: %w", err)
	}
	return &m, nil
}
