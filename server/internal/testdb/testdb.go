// Package testdb provides the shared availability gate for DB-backed tests.
package testdb

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultDatabaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	OptionalEnv        = "MULTICA_TEST_DB_OPTIONAL"
	probeTimeout       = 3 * time.Second
)

// DatabaseURL resolves the same database target used by the DB-backed suites.
func DatabaseURL() string {
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		return databaseURL
	}
	return DefaultDatabaseURL
}

// Require verifies that the resolved test database is reachable. The only
// fail-open path is the explicit contributor opt-out, which prints a banner so
// its skipped DB coverage cannot be mistaken for a green run.
func Require(ctx context.Context, output io.Writer) error {
	databaseURL := DatabaseURL()
	if err := probe(ctx, databaseURL); err == nil {
		return nil
	}
	return FailOrSkip(output, "test database unavailable")
}

// FailOrSkip applies the shared fail-closed policy to a known DB-backed test
// setup failure that occurs after the initial availability probe.
func FailOrSkip(output io.Writer, reason string) error {
	target := redactedURL(DatabaseURL())
	if os.Getenv(OptionalEnv) == "1" {
		fmt.Fprintf(output, "WARNING: %s=1 is set; DB-backed suites did not run because %s at %s.\n", OptionalEnv, reason, target)
		return nil
	}

	return fmt.Errorf("%s at %s. Remedy: start and migrate Postgres at DATABASE_URL, or set %s=1 to explicitly skip DB-backed suites", reason, target, OptionalEnv)
}

// ExitFailure terminates a TestMain through the shared fail-closed policy.
// It returns success only for the explicit opt-out, after printing its banner.
func ExitFailure(reason string) {
	if err := FailOrSkip(os.Stdout, reason); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Main is the TestMain implementation for packages whose existing tests own
// their pools and fixtures. Packages with additional TestMain setup call
// Require directly before preserving that setup path.
func Main(m *testing.M) {
	if err := Require(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func probe(ctx context.Context, databaseURL string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return pool.Ping(ctx)
}

func redactedURL(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "<invalid DATABASE_URL; credentials redacted>"
	}
	if parsed.User != nil {
		parsed.User = url.User("REDACTED")
	}
	return parsed.String()
}
