package testdb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDatabaseURL(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		if got := DatabaseURL(); got != DefaultDatabaseURL {
			t.Fatalf("DatabaseURL() = %q, want %q", got, DefaultDatabaseURL)
		}
	})

	t.Run("environment", func(t *testing.T) {
		const databaseURL = "postgres://user:secret@db.example.test:5432/testdb?sslmode=disable"
		t.Setenv("DATABASE_URL", databaseURL)
		if got := DatabaseURL(); got != databaseURL {
			t.Fatalf("DatabaseURL() = %q, want %q", got, databaseURL)
		}
	})
}

func TestRequire(t *testing.T) {
	const (
		databaseURL = "postgres://probe-user:probe-secret@127.0.0.1:1/multica?sslmode=disable"
		redactedURL = "postgres://REDACTED@127.0.0.1:1/multica?sslmode=disable"
	)

	t.Run("fails closed with a redacted target and remedy", func(t *testing.T) {
		t.Setenv("DATABASE_URL", databaseURL)
		t.Setenv(OptionalEnv, "")
		var output bytes.Buffer

		err := Require(context.Background(), &output)
		if err == nil {
			t.Fatal("Require() returned nil for an unreachable database")
		}
		message := err.Error()
		for _, want := range []string{redactedURL, "Remedy:", OptionalEnv + "=1"} {
			if !strings.Contains(message, want) {
				t.Errorf("error %q does not contain %q", message, want)
			}
		}
		if strings.Contains(message, "probe-secret") {
			t.Fatalf("error exposed database credentials: %q", message)
		}
		if output.Len() != 0 {
			t.Fatalf("fail-closed path wrote success output: %q", output.String())
		}
	})

	t.Run("explicit opt-out prints a loud banner", func(t *testing.T) {
		t.Setenv("DATABASE_URL", databaseURL)
		t.Setenv(OptionalEnv, "1")
		var output bytes.Buffer

		if err := Require(context.Background(), &output); err != nil {
			t.Fatalf("Require() with opt-out returned error: %v", err)
		}
		banner := output.String()
		for _, want := range []string{"WARNING", OptionalEnv + "=1", redactedURL, "DB-backed suites did not run"} {
			if !strings.Contains(banner, want) {
				t.Errorf("banner %q does not contain %q", banner, want)
			}
		}
		if strings.Contains(banner, "probe-secret") {
			t.Fatalf("banner exposed database credentials: %q", banner)
		}
	})
}
