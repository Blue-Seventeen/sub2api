// Command migration-rehearsal checks upgrades on an explicitly named disposable database.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	_ "github.com/lib/pq"
)

type invariant struct {
	table  string
	fields []string
}

var invariants = []invariant{
	{"users", []string{"id", "balance", "status", "unified_rate_enabled", "unified_rate_multiplier"}},
	{"groups", []string{"id", "platform", "rate_multiplier", "models_list_config", "peak_rate_windows", "peak_start", "peak_end", "peak_rate_multiplier", "video_price_480p", "video_price_720p", "video_price_1080p"}},
	{"api_keys", []string{"id", "user_id", "group_id", "quota", "quota_used", "status", "key"}},
	{"user_subscriptions", nil},
	{"user_group_rates", nil},
	{"user_platform_quotas", nil},
	{"composite_model_routes", nil},
}

func snapshot(ctx context.Context, db *sql.DB, spec invariant) (string, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", spec.table).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		return "absent", nil
	}
	projection := "to_jsonb(t) - 'updated_at'"
	if len(spec.fields) > 0 {
		parts := make([]string, 0, len(spec.fields))
		for _, field := range spec.fields {
			parts = append(parts, "to_jsonb(t)->'"+field+"'")
		}
		projection = "jsonb_build_array(" + strings.Join(parts, ",") + ")"
	}
	rows, err := db.QueryContext(ctx, "SELECT ("+projection+")::text FROM "+spec.table+" t ORDER BY 1")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hash := sha256.New()
	count := 0
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			return "", err
		}
		fmt.Fprintln(hash, row)
		count++
	}
	return fmt.Sprintf("%d:%x", count, hash.Sum(nil)), rows.Err()
}

func migrationLedgerSnapshot(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, "SELECT filename, checksum FROM schema_migrations ORDER BY filename")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hash := sha256.New()
	count := 0
	for rows.Next() {
		var filename, checksum string
		if err := rows.Scan(&filename, &checksum); err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%s\n", filename, checksum)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%x", count, hash.Sum(nil)), nil
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", os.Getenv("SUB2API_REHEARSAL_DSN"))
	if err != nil {
		return fmt.Errorf("open rehearsal database failed")
	}
	defer db.Close()
	var name string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return fmt.Errorf("connect rehearsal database failed")
	}
	if !strings.HasSuffix(name, "_rehearsal") {
		return fmt.Errorf("refusing migration: database must end in _rehearsal")
	}
	rows, err := db.QueryContext(ctx, "SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename LIKE 'promotion%' ORDER BY tablename")
	if err != nil {
		return err
	}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return err
		}
		invariants = append(invariants, invariant{table, nil})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	before := make(map[string]string)
	for _, spec := range invariants {
		before[spec.table], err = snapshot(ctx, db, spec)
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", spec.table, err)
		}
	}
	ledgerBefore, err := migrationLedgerSnapshot(ctx, db)
	if err != nil {
		return fmt.Errorf("snapshot schema_migrations before upgrade: %w", err)
	}
	var ledgerAfterFirst string
	for pass := 1; pass <= 2; pass++ {
		if err := repository.ApplyMigrations(ctx, db); err != nil {
			return fmt.Errorf("migration pass %d: %w", pass, err)
		}
		if pass == 1 {
			ledgerAfterFirst, err = migrationLedgerSnapshot(ctx, db)
			if err != nil {
				return fmt.Errorf("snapshot schema_migrations after first pass: %w", err)
			}
		}
		fmt.Printf("migration pass %d passed\n", pass)
	}
	ledgerAfterSecond, err := migrationLedgerSnapshot(ctx, db)
	if err != nil {
		return fmt.Errorf("snapshot schema_migrations after second pass: %w", err)
	}
	if ledgerBefore == ledgerAfterFirst {
		return fmt.Errorf("migration ledger did not advance during upgrade")
	}
	if ledgerAfterFirst != ledgerAfterSecond {
		return fmt.Errorf("migration ledger changed during idempotency pass")
	}
	for _, spec := range invariants {
		after, err := snapshot(ctx, db, spec)
		if err != nil {
			return err
		}
		if before[spec.table] != after {
			return fmt.Errorf("preservation invariant changed: %s", spec.table)
		}
		fmt.Printf("preserved %s %s\n", spec.table, after)
	}
	var invalid int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND (NOT i.indisvalid OR NOT i.indisready)").Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("invalid or unready indexes: %d", invalid)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	groups, err := client.Group.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("merged Ent group query: %w", err)
	}
	fmt.Printf("merged Ent group query passed (%d groups), all indexes valid\n", len(groups))
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
