package main

import (
	"context"
	"fmt"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyConnectError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want connectState
	}{
		{name: "success", err: nil, want: connectReady},
		{name: "connection refused", err: syscall.ECONNREFUSED, want: connectNothingListening},
		{name: "wrapped connection refused", err: fmt.Errorf("dial database: %w", syscall.ECONNREFUSED), want: connectNothingListening},
		{name: "authentication failure", err: &pgconn.PgError{Code: "28P01"}, want: connectFailure},
		{name: "timeout", err: context.DeadlineExceeded, want: connectFailure},
		{name: "unknown failure", err: fmt.Errorf("unexpected transport failure"), want: connectFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyConnectError(tt.err); got != tt.want {
				t.Fatalf("classifyConnectError(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestEffectiveEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		databaseURL string
		want        string
		wantErr     bool
	}{
		{
			name:        "authority endpoint",
			databaseURL: "postgres://dev:dev@127.0.0.1:25432/postgres?sslmode=disable",
			want:        "127.0.0.1:25432",
		},
		{
			name:        "query overrides authority port",
			databaseURL: "postgres://dev:dev@127.0.0.1:25432/postgres?sslmode=disable&port=29998",
			want:        "127.0.0.1:29998",
		},
		{
			name:        "multiple endpoints rejected",
			databaseURL: "postgres://dev:dev@127.0.0.1:25432/postgres?sslmode=disable&host=127.0.0.1,127.0.0.2",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config, err := pgx.ParseConfig(tt.databaseURL)
			if err != nil {
				t.Fatalf("pgx.ParseConfig() error = %v", err)
			}
			got, err := effectiveEndpoint(config)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("effectiveEndpoint() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("effectiveEndpoint() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("effectiveEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQuoteIdentifier(t *testing.T) {
	t.Parallel()

	if got, want := quoteIdentifier(`dev"database`), `"dev""database"`; got != want {
		t.Fatalf("quoteIdentifier() = %q, want %q", got, want)
	}
}
