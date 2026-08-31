package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	exitSuccess = 0
	exitFailure = 1
)

type connectState int

const (
	connectReady connectState = iota
	connectNothingListening
	connectFailure
)

func main() {
	os.Exit(run())
}

func run() int {
	databaseURL := os.Getenv("DATABASE_URL")
	databaseName := os.Getenv("POSTGRES_DB")
	expectedSystemID := strings.TrimSpace(os.Getenv("EXPECTED_SYSTEM_ID"))
	if databaseURL == "" || databaseName == "" {
		return fail("DATABASE_URL and POSTGRES_DB are required", nil)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		if classifyConnectError(err) == connectNothingListening {
			fmt.Fprintln(os.Stdout, "nothing-listening")
			fmt.Fprintln(os.Stderr, "no process is listening at DATABASE_URL")
			return exitSuccess
		}
		return fail("database probe failed", err)
	}
	defer func() {
		if err := conn.Close(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "close database probe connection: %v\n", err)
		}
	}()

	var reachedSystemID string
	if err := conn.QueryRow(ctx,
		"SELECT system_identifier::text FROM pg_control_system()",
	).Scan(&reachedSystemID); err != nil {
		return fail("read PostgreSQL system identifier", err)
	}
	if expectedSystemID == "" {
		return fail("DATABASE_URL reached PostgreSQL, but this Compose project's identity could not be read", nil)
	}
	if reachedSystemID != expectedSystemID {
		return fail("DATABASE_URL does not reach this Compose project's PostgreSQL", nil)
	}

	var exists bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)",
		databaseName,
	).Scan(&exists); err != nil {
		return fail("check development database", err)
	}
	if exists {
		fmt.Fprintln(os.Stdout, "ready")
		fmt.Fprintln(os.Stdout, "PostgreSQL identity verified through DATABASE_URL.")
		return exitSuccess
	}

	if _, err := conn.Exec(ctx, "CREATE DATABASE "+quoteIdentifier(databaseName)); err != nil {
		return fail("create development database", err)
	}
	fmt.Fprintln(os.Stdout, "ready")
	fmt.Fprintf(os.Stdout, "Created database %s through DATABASE_URL.\n", databaseName)
	return exitSuccess
}

func classifyConnectError(err error) connectState {
	if err == nil {
		return connectReady
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return connectNothingListening
	}
	return connectFailure
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func fail(message string, err error) int {
	if err == nil {
		fmt.Fprintln(os.Stderr, message)
	} else {
		fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)
	}
	return exitFailure
}
