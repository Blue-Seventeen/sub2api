// Package ent provides the generated ORM code for database entities.
package ent

// Enable sql/execquery to generate ExecContext/QueryContext passthrough helpers for raw SQL in tx.
// Enable sql/lock to support FOR UPDATE row locking.
//go:generate go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,intercept,sql/execquery,sql/lock --idtype int64 ./schema
