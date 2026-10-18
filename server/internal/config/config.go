// Package config reads the runtime settings from the environment (docs/contracts/layout.md).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the runtime configuration of the API server.
type Config struct {
	Port           string   // PORT, default 8080
	DatabaseURL    string   // DATABASE_URL
	StaticDir      string   // STATIC_DIR, default "."
	AllowedOrigins []string // ALLOWED_ORIGINS, comma-separated; "*" allows any origin
	AdminUser      string   // ADMIN_USER
	AdminPassword  string   // ADMIN_PASSWORD
	Build          string   // BUILD, default "dev"
	// TrustProxy is the number of reverse proxies in front of the server whose X-Forwarded-For and
	// X-Forwarded-Proto are believed (TRUST_PROXY: 0 or empty = none, 1 or "true" = one, e.g. Caddy).
	TrustProxy int
	LogLevel   string // LOG_LEVEL: debug, info (default), warn, error
}

// Defaults from layout.md.
const (
	DefaultPort        = "8080"
	DefaultDatabaseURL = "postgres://mq:mq@localhost:5432/mq?sslmode=disable"
	DefaultStaticDir   = "."
	DefaultBuild       = "dev"
)

// FromEnv reads the configuration with os.Getenv.
func FromEnv() (Config, error) { return Load(os.Getenv) }

// Load reads the configuration through getenv (so tests can pass a map).
func Load(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	c := Config{
		Port:          get("PORT", DefaultPort),
		DatabaseURL:   get("DATABASE_URL", DefaultDatabaseURL),
		StaticDir:     get("STATIC_DIR", DefaultStaticDir),
		AdminUser:     getenv("ADMIN_USER"),
		AdminPassword: getenv("ADMIN_PASSWORD"),
		Build:         get("BUILD", DefaultBuild),
		LogLevel:      strings.ToLower(get("LOG_LEVEL", "info")),
	}
	if p, err := strconv.Atoi(c.Port); err != nil || p < 1 || p > 65535 {
		return c, fmt.Errorf("config: PORT %q is not a port number", c.Port)
	}
	for _, o := range strings.Split(getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}
	switch tp := strings.ToLower(strings.TrimSpace(getenv("TRUST_PROXY"))); tp {
	case "", "0", "false", "no":
	case "true", "yes":
		c.TrustProxy = 1
	default:
		n, err := strconv.Atoi(tp)
		if err != nil || n < 0 || n > 10 {
			return c, fmt.Errorf("config: TRUST_PROXY %q must be a number of proxies (0-10) or true/false", tp)
		}
		c.TrustProxy = n
	}
	return c, nil
}

// AdminEnabled reports whether /admin is served (both credentials set).
func (c Config) AdminEnabled() bool { return c.AdminUser != "" && c.AdminPassword != "" }
