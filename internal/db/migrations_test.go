package db

import (
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	// Blank-import the pgx5 database driver so the URL scheme registers
	// for the optional live round-trip below.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// migrationName matches "0001_init.up.sql" / "0007_agent_network_telemetry.down.sql".
var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// TestMigrationFilesArePaired walks the embedded migrations FS and
// makes sure every numbered migration has both an up and a down file
// with matching slugs, and that numbers are contiguous starting at
// 0001. This catches typos like 0007_x.up.sql / 0007_y.down.sql long
// before they reach a real Postgres.
//
// This is the minimum bar for the "migration round-trip" test the
// plan calls for. The optional second half (TestMigrationRoundTrip
// below) actually exercises up→down→up against a real database when
// SONAR_TEST_DSN is set.
func TestMigrationFilesArePaired(t *testing.T) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}

	// version → side → slug
	type pair struct{ up, down string }
	versions := map[string]*pair{}
	var versionList []int

	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("migration %q does not match NNNN_slug.{up,down}.sql", e.Name())
			continue
		}
		num, slug, side := m[1], m[2], m[3]
		key := num
		if versions[key] == nil {
			versions[key] = &pair{}
			versionList = append(versionList, atoi(t, num))
		}
		switch side {
		case "up":
			if versions[key].up != "" {
				t.Errorf("duplicate up migration for version %s: %s vs %s", num, versions[key].up, slug)
			}
			versions[key].up = slug
		case "down":
			if versions[key].down != "" {
				t.Errorf("duplicate down migration for version %s: %s vs %s", num, versions[key].down, slug)
			}
			versions[key].down = slug
		}
	}
	if len(versionList) == 0 {
		t.Fatal("no migrations found in embedded FS")
	}
	sort.Ints(versionList)
	for i, v := range versionList {
		want := i + 1
		if v != want {
			t.Errorf("expected version %04d at position %d, got %04d (gap or out-of-order)", want, i, v)
		}
		key := strings.Repeat("0", 4-len(itoa(v))) + itoa(v)
		p := versions[key]
		if p.up == "" {
			t.Errorf("version %s has no up.sql", key)
		}
		if p.down == "" {
			t.Errorf("version %s has no down.sql", key)
		}
		if p.up != "" && p.down != "" && p.up != p.down {
			t.Errorf("version %s: up slug %q != down slug %q", key, p.up, p.down)
		}
	}
}

// TestMigrationRoundTrip drives migrate up, then down, then up again
// against a real Postgres. Skipped unless SONAR_TEST_DSN is set —
// the round-trip needs an actual server because the migrations
// reference Timescale extension functions and CREATE EXTENSION.
//
// Example:
//
//	SONAR_TEST_DSN='pgx5://sonar:sonar@localhost:5432/sonar_test?sslmode=disable' go test ./internal/db/...
func TestMigrationRoundTrip(t *testing.T) {
	dsn := os.Getenv("SONAR_TEST_DSN")
	if dsn == "" {
		t.Skip("SONAR_TEST_DSN not set; skipping live migration round-trip")
	}

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	src, err := iofs.New(sub, ".")
	if err != nil {
		t.Fatalf("iofs: %v", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("first up: %v", err)
	}
	if err := m.Down(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("down: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("second up: %v", err)
	}
}

func atoi(t *testing.T, s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("non-numeric version: %s", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
