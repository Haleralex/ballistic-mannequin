package config

import "testing"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "8080" || c.DatabaseURL != DefaultDatabaseURL || c.StaticDir != "." || c.Build != "dev" ||
		c.TrustProxy != 0 || len(c.AllowedOrigins) != 0 || c.AdminEnabled() {
		t.Fatalf("%+v", c)
	}
}

func TestValues(t *testing.T) {
	c, err := Load(env(map[string]string{
		"PORT": "9000", "DATABASE_URL": "postgres://x", "STATIC_DIR": "/srv", "BUILD": "abc1234",
		"ALLOWED_ORIGINS": " https://a.github.io/ , ,https://b.ru", "ADMIN_USER": "u", "ADMIN_PASSWORD": "p",
		"TRUST_PROXY": "1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "9000" || c.DatabaseURL != "postgres://x" || c.StaticDir != "/srv" || c.Build != "abc1234" ||
		c.TrustProxy != 1 || !c.AdminEnabled() {
		t.Fatalf("%+v", c)
	}
	if len(c.AllowedOrigins) != 2 || c.AllowedOrigins[0] != "https://a.github.io" || c.AllowedOrigins[1] != "https://b.ru" {
		t.Fatalf("origins %q", c.AllowedOrigins)
	}
}

func TestTrustProxy(t *testing.T) {
	for in, want := range map[string]int{"": 0, "0": 0, "false": 0, "true": 1, "1": 1, "2": 2} {
		c, err := Load(env(map[string]string{"TRUST_PROXY": in}))
		if err != nil || c.TrustProxy != want {
			t.Errorf("%q: %d %v", in, c.TrustProxy, err)
		}
	}
	if _, err := Load(env(map[string]string{"TRUST_PROXY": "maybe"})); err == nil {
		t.Error("bad TRUST_PROXY accepted")
	}
	if _, err := Load(env(map[string]string{"PORT": "http"})); err == nil {
		t.Error("bad PORT accepted")
	}
}

func TestAdminNeedsBoth(t *testing.T) {
	c, _ := Load(env(map[string]string{"ADMIN_USER": "u"}))
	if c.AdminEnabled() {
		t.Fatal("admin enabled without password")
	}
}
