// Command migrate-sqlite copies the account and check history from the old
// SQLite database into PostgreSQL. It is safe to run more than once: existing
// rows are left untouched by ON CONFLICT DO NOTHING.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func main() {
	sqlitePath := flag.String("sqlite", "soglia.sqlite.backup", "path to the old SQLite database")
	pgURL := flag.String("pg", envOr("SOGLIA_DATABASE_URL", envOr("DATABASE_URL", "postgres://soglia:soglia@localhost:5432/soglia?sslmode=verify-full&sslrootcert=certs/ca.crt")), "PostgreSQL connection URL")
	flag.Parse()

	if _, err := os.Stat(*sqlitePath); err != nil {
		log.Fatalf("SQLite database %q is not available: %v", *sqlitePath, err)
	}

	source, err := sql.Open("sqlite", *sqlitePath)
	if err != nil {
		log.Fatalf("open SQLite: %v", err)
	}
	defer source.Close()

	target, err := sql.Open("pgx", *pgURL)
	if err != nil {
		log.Fatalf("open Postgres: %v", err)
	}
	defer target.Close()

	// The API applies schema migrations on startup; require that first.
	if _, err := target.Exec(`SELECT 1 FROM schema_migrations LIMIT 1`); err != nil {
		log.Fatalf("target database has no schema; start the API once first: %v", err)
	}

	tx, err := target.Begin()
	if err != nil {
		log.Fatalf("begin import: %v", err)
	}
	defer tx.Rollback()

	users := 0
	userRows, err := source.Query(`SELECT id, username, salt, password_hash, created_at FROM users`)
	if err != nil {
		log.Fatalf("read SQLite users: %v", err)
	}
	for userRows.Next() {
		var id, created int64
		var username, salt, hash string
		if err := userRows.Scan(&id, &username, &salt, &hash, &created); err != nil {
			log.Fatalf("scan SQLite user: %v", err)
		}
		if _, err := tx.Exec(`INSERT INTO users (id, username, salt, password_hash, created_at)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO NOTHING`, id, username, salt, hash, created); err != nil {
			log.Fatalf("import user: %v", err)
		}
		users++
	}
	userRows.Close()
	if err := userRows.Err(); err != nil {
		log.Fatalf("iterate SQLite users: %v", err)
	}

	checks := 0
	checkRows, err := source.Query(`SELECT id, text, action, tier, prob, ms, matched, created_at FROM checks`)
	if err != nil {
		log.Fatalf("read SQLite checks: %v", err)
	}
	for checkRows.Next() {
		var id, created int64
		var text, action, tier, matched string
		var prob, ms float64
		if err := checkRows.Scan(&id, &text, &action, &tier, &prob, &ms, &matched, &created); err != nil {
			log.Fatalf("scan SQLite check: %v", err)
		}
		if _, err := tx.Exec(`INSERT INTO checks (id, text, action, tier, prob, ms, matched, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (id) DO NOTHING`,
			id, text, action, tier, prob, ms, matched, created); err != nil {
			log.Fatalf("import check: %v", err)
		}
		checks++
	}
	checkRows.Close()
	if err := checkRows.Err(); err != nil {
		log.Fatalf("iterate SQLite checks: %v", err)
	}

	if _, err := tx.Exec(`SELECT setval(pg_get_serial_sequence('checks', 'id'),
		GREATEST(COALESCE((SELECT MAX(id) FROM checks), 1), 1))`); err != nil {
		log.Fatalf("reset id sequence: %v", err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatalf("commit import: %v", err)
	}

	fmt.Printf("migration complete: %d user(s), %d check(s) imported from %s\n", users, checks, *sqlitePath)
}
