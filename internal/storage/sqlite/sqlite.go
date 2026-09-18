// Package sqlite implements the application's stores on top of SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver" // registers the "sqlite3" driver
	"github.com/pressly/goose/v3"

	"github.com/feytox/kabanbot/internal/storage/sqlite/sqlcgen"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB is an open, migrated database.
type DB struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

// Open opens the database at path, creating it if needed, and applies migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	return open(ctx, "file:"+filepath.ToSlash(path))
}

func open(ctx context.Context, dsn string) (*DB, error) {
	sep := "?"
	if u, err := url.Parse(dsn); err == nil && u.RawQuery != "" {
		sep = "&"
	}
	dsn += sep + "_txlock=immediate" +
		"&_pragma=journal_mode(wal)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(on)" +
		"&_pragma=synchronous(normal)"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{db: db, q: sqlcgen.New(db)}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Ping checks that the database is reachable.
func (d *DB) Ping(ctx context.Context) error { return d.db.PingContext(ctx) }

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }
