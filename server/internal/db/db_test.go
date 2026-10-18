package db_test

import (
	"context"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/Haleralex/ballistic-mannequin/server/internal/db"
	"github.com/Haleralex/ballistic-mannequin/server/internal/dbtest"
	"github.com/Haleralex/ballistic-mannequin/server/migrations"
)

func TestLoadOrdersAndValidatesNames(t *testing.T) {
	migs, err := db.Load(fstest.MapFS{
		"010_c.sql":  {Data: []byte("c")},
		"002_b.sql":  {Data: []byte("b")},
		"001_a.sql":  {Data: []byte("a")},
		"README.txt": {Data: []byte("x")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) != 3 || migs[0].Version != 1 || migs[1].Version != 2 || migs[2].Version != 10 {
		t.Fatalf("%+v", migs)
	}
	if _, err := db.Load(fstest.MapFS{"abc.sql": {}}); err == nil {
		t.Error("unnumbered migration accepted")
	}
	if _, err := db.Load(fstest.MapFS{"001_a.sql": {}, "1_b.sql": {}}); err == nil {
		t.Error("duplicate version accepted")
	}
}

func TestEmbeddedMigrationsLoad(t *testing.T) {
	migs, err := db.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) < 3 || migs[0].Name != "001_schema.sql" {
		t.Fatalf("%+v", migs)
	}
}

func TestMigrateFreshAndIdempotent(t *testing.T) {
	pool, _ := dbtest.NewWithSchema(t, false)
	ctx := context.Background()
	done, err := db.Migrate(ctx, pool, migrations.FS, dbtest.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	all, _ := db.Load(migrations.FS)
	if len(done) != len(all) {
		t.Fatalf("applied %v", done)
	}
	again, err := db.Migrate(ctx, pool, migrations.FS, dbtest.Quiet())
	if err != nil || len(again) != 0 {
		t.Fatalf("second run applied %v, err %v", again, err)
	}

	// The seeded experiments of events.md.
	rows, err := pool.Query(ctx, "SELECT key, variants, weights, metric, active FROM experiments ORDER BY key")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type exp struct {
		variants []string
		weights  []int32
		metric   string
		active   bool
	}
	got := map[string]exp{}
	for rows.Next() {
		var k string
		var e exp
		if err := rows.Scan(&k, &e.variants, &e.weights, &e.metric, &e.active); err != nil {
			t.Fatal(err)
		}
		got[k] = e
	}
	want := map[string]exp{
		"t1_positioning":  {[]string{"a", "b", "c"}, []int32{1, 1, 1}, "activated", true},
		"t2_onboarding":   {[]string{"a", "b"}, []int32{1, 1}, "activated", true},
		"t3_share_prompt": {[]string{"a", "b"}, []int32{1, 1}, "shared", false},
		"t4_price":        {[]string{"a", "b"}, []int32{1, 1}, "fake_door_click", false},
	}
	if len(got) != len(want) {
		t.Fatalf("experiments %v", got)
	}
	for k, w := range want {
		g := got[k]
		if len(g.variants) != len(w.variants) || g.metric != w.metric || g.active != w.active || len(g.weights) != len(w.weights) {
			t.Errorf("%s: got %+v want %+v", k, g, w)
		}
	}
}

func TestMigrateConcurrently(t *testing.T) {
	pool, _ := dbtest.NewWithSchema(t, false)
	ctx := context.Background()
	var wg sync.WaitGroup
	total := make(chan int, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			done, err := db.Migrate(ctx, pool, migrations.FS, dbtest.Quiet())
			errs <- err
			total <- len(done)
		}()
	}
	wg.Wait()
	close(errs)
	close(total)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	sum := 0
	for n := range total {
		sum += n
	}
	all, _ := db.Load(migrations.FS)
	if sum != len(all) {
		t.Fatalf("migrations applied %d times in total, want %d", sum, len(all))
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	pool, _ := dbtest.NewWithSchema(t, false)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"001_ok.sql":  {Data: []byte("CREATE TABLE ok (id int);")},
		"002_bad.sql": {Data: []byte("CREATE TABLE half (id int); SELECT no_such_function();")},
	}
	if _, err := db.Migrate(ctx, pool, fsys, dbtest.Quiet()); err == nil {
		t.Fatal("bad migration succeeded")
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema_migrations has %d rows (%v)", n, err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('half') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("half-applied migration left table behind: %v %v", exists, err)
	}
}
