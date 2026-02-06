// Package storage handles data persistence: SQLite database and filesystem.
package storage

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3" // Blank import: registers the SQLite driver.
	// In Go, importing a package for its side effects (init function) is done
	// with `_`. The sqlite3 package registers itself as a database/sql driver.
)

// Schema is embedded directly in the binary using Go's embed feature.
// This means no migration files need to exist at runtime — they're baked into the binary.
const schema = `
CREATE TABLE IF NOT EXISTS logos (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol        TEXT NOT NULL UNIQUE,
    company_name  TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL DEFAULT 'unknown',
    original_url  TEXT NOT NULL DEFAULT '',
    has_xs        BOOLEAN NOT NULL DEFAULT 0,
    has_s         BOOLEAN NOT NULL DEFAULT 0,
    has_m         BOOLEAN NOT NULL DEFAULT 0,
    has_l         BOOLEAN NOT NULL DEFAULT 0,
    has_xl        BOOLEAN NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS llm_calls (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol      TEXT NOT NULL,
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    result_url  TEXT,
    success     BOOLEAN NOT NULL DEFAULT 0,
    duration_ms INTEGER,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_logos_symbol ON logos(symbol);
CREATE INDEX IF NOT EXISTS idx_logos_status ON logos(status);
CREATE INDEX IF NOT EXISTS idx_llm_calls_symbol ON llm_calls(symbol);
`

// NewDatabase creates a new SQLite connection and runs migrations.
// sqlx wraps database/sql with convenience methods like StructScan and NamedExec.
//
// Key Go pattern: the constructor creates the resource AND validates it (Ping).
// If anything fails, we return an error — the caller decides what to do.
func NewDatabase(dbPath string) (*sqlx.DB, error) {
	// The DSN (Data Source Name) configures SQLite pragmas for better performance:
	// - WAL mode: allows concurrent reads while writing
	// - foreign_keys: enforce referential integrity
	// - busy_timeout: wait up to 5s instead of failing on lock contention
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", dbPath)

	db, err := sqlx.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Ping actually opens the connection (Open is lazy in database/sql)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// SQLite performs best with a single writer connection
	db.SetMaxOpenConns(1)

	// Run migrations
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}
