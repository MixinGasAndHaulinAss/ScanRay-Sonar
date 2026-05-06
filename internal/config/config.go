// Package config loads runtime configuration from environment variables.
// Every key is prefixed SONAR_ to avoid collisions on shared hosts.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-parsed runtime configuration. Populated once at startup
// by Load(); all subsequent reads should be from this struct (no env reads
// elsewhere in the codebase).
type Config struct {
	Env       string
	LogLevel  string
	BindAddr  string
	PublicURL string
	IngestURL string

	// CollectorImage is the canonical image reference operators pull
	// when standing up a remote sonar-collector. Surfaced in the
	// Collectors UI so install commands match whatever registry this
	// deployment publishes to (overrideable via SONAR_COLLECTOR_IMAGE
	// for air-gapped or fork installs).
	CollectorImage string

	MasterKeyB64  string // 32 bytes, base64 encoded
	JWTSecretB64  string // 64 bytes, base64 encoded
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration

	DB struct {
		Host     string
		Port     int
		Name     string
		User     string
		Password string
		SSLMode  string
		MaxOpen  int
		MaxIdle  int
	}

	NATSURL string

	SMTP struct {
		Host     string
		Port     int
		User     string
		Password string
		From     string
		TLS      bool
	}

	BootstrapAdminEmail    string
	BootstrapAdminPassword string

	// GeoIPCityPath / GeoIPASNPath point to MaxMind GeoLite2 .mmdb
	// files mounted into the container (typically under
	// /var/lib/sonar/geoip via the sonar-geoip volume). Both are
	// optional: when missing the API simply doesn't enrich snapshots
	// with geo/ASN data and the world map renders an "unknown"
	// pin set.
	GeoIPCityPath string
	GeoIPASNPath  string
}

// Load reads the environment, applies defaults, and validates required keys.
// Returns an aggregated error listing every missing or invalid value so
// operators can fix them all in one pass.
func Load() (*Config, error) {
	c := &Config{
		Env:            env("SONAR_ENV", "production"),
		LogLevel:       env("SONAR_LOG_LEVEL", "info"),
		BindAddr:       env("SONAR_BIND_ADDR", "127.0.0.1:8080"),
		PublicURL:      env("SONAR_PUBLIC_URL", ""),
		IngestURL:      env("SONAR_INGEST_URL", ""),
		CollectorImage: env("SONAR_COLLECTOR_IMAGE", ""),
		MasterKeyB64:   env("SONAR_MASTER_KEY", ""),
		JWTSecretB64:   env("SONAR_JWT_SECRET", ""),
		NATSURL:        env("SONAR_NATS_URL", "nats://127.0.0.1:4222"),

		BootstrapAdminEmail:    env("SONAR_BOOTSTRAP_ADMIN_EMAIL", ""),
		BootstrapAdminPassword: env("SONAR_BOOTSTRAP_ADMIN_PASSWORD", ""),

		GeoIPCityPath: env("SONAR_GEOIP_CITY_DB", "/var/lib/sonar/geoip/GeoLite2-City.mmdb"),
		GeoIPASNPath:  env("SONAR_GEOIP_ASN_DB", "/var/lib/sonar/geoip/GeoLite2-ASN.mmdb"),
	}

	var errs []string

	c.JWTAccessTTL = mustDuration(env("SONAR_JWT_ACCESS_TTL", "15m"), &errs)
	c.JWTRefreshTTL = mustDuration(env("SONAR_JWT_REFRESH_TTL", "720h"), &errs)

	c.DB.Host = env("SONAR_DB_HOST", "127.0.0.1")
	c.DB.Port = mustInt(env("SONAR_DB_PORT", "5432"), &errs)
	c.DB.Name = env("SONAR_DB_NAME", "sonar")
	c.DB.User = env("SONAR_DB_USER", "sonar")
	c.DB.Password = env("SONAR_DB_PASSWORD", "")
	c.DB.SSLMode = env("SONAR_DB_SSLMODE", "disable")
	c.DB.MaxOpen = mustInt(env("SONAR_DB_MAX_OPEN", "25"), &errs)
	c.DB.MaxIdle = mustInt(env("SONAR_DB_MAX_IDLE", "5"), &errs)

	c.SMTP.Host = env("SONAR_SMTP_HOST", "")
	c.SMTP.Port = mustInt(env("SONAR_SMTP_PORT", "587"), &errs)
	c.SMTP.User = env("SONAR_SMTP_USER", "")
	c.SMTP.Password = env("SONAR_SMTP_PASSWORD", "")
	c.SMTP.From = env("SONAR_SMTP_FROM", "")
	c.SMTP.TLS = strings.EqualFold(env("SONAR_SMTP_TLS", "true"), "true")

	if c.MasterKeyB64 == "" {
		errs = append(errs, "SONAR_MASTER_KEY is required (32 random bytes, base64). Generate with: openssl rand -base64 32")
	}
	if c.JWTSecretB64 == "" {
		errs = append(errs, "SONAR_JWT_SECRET is required (64 random bytes, base64). Generate with: openssl rand -base64 64")
	}
	if c.DB.Password == "" {
		errs = append(errs, "SONAR_DB_PASSWORD is required")
	}

	if len(errs) > 0 {
		return nil, errors.New("config: " + strings.Join(errs, "; "))
	}
	return c, nil
}

// PostgresDSN renders a libpq-style connection string for pgx.
func (c *Config) PostgresDSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.DB.Host, c.DB.Port, c.DB.User, c.DB.Password, c.DB.Name, c.DB.SSLMode,
	)
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func mustInt(s string, errs *[]string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("invalid integer %q: %v", s, err))
		return 0
	}
	return n
}

func mustDuration(s string, errs *[]string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("invalid duration %q: %v", s, err))
		return 0
	}
	return d
}
