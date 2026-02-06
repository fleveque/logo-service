package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/fleveque/logo-service/internal/model"
)

// ErrNotFound is returned when a logo doesn't exist in the database.
// Go uses sentinel errors (predefined error values) instead of exception types.
// Callers check with errors.Is(err, ErrNotFound).
var ErrNotFound = errors.New("logo not found")

// LogoRepository defines the interface for logo persistence.
// Go interfaces are implicit — any struct that has these methods satisfies it.
// This makes testing easy: you can create a mock that implements this interface
// without importing anything from the real implementation.
type LogoRepository interface {
	GetBySymbol(ctx context.Context, symbol string) (*model.Logo, error)
	Create(ctx context.Context, logo *model.Logo) error
	Update(ctx context.Context, logo *model.Logo) error
	SetSizeAvailable(ctx context.Context, symbol string, size model.LogoSize) error
	SetStatus(ctx context.Context, symbol string, status model.LogoStatus, errMsg string) error
	Count(ctx context.Context) (int64, error)
	CountByStatus(ctx context.Context, status model.LogoStatus) (int64, error)
	ListPending(ctx context.Context, limit int) ([]model.Logo, error)
}

// sqliteLogoRepository is the SQLite implementation of LogoRepository.
// The struct is unexported (lowercase first letter) — only the interface is public.
// This is a common Go pattern: export the interface, hide the implementation.
type sqliteLogoRepository struct {
	db *sqlx.DB
}

// NewLogoRepository creates a new SQLite-backed LogoRepository.
func NewLogoRepository(db *sqlx.DB) LogoRepository {
	return &sqliteLogoRepository{db: db}
}

func (r *sqliteLogoRepository) GetBySymbol(ctx context.Context, symbol string) (*model.Logo, error) {
	var logo model.Logo
	// sqlx.GetContext scans the result row directly into the struct using `db:` tags.
	// context.Context carries request-scoped data like deadlines and cancellation.
	err := r.db.GetContext(ctx, &logo, "SELECT * FROM logos WHERE symbol = ?", symbol)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting logo by symbol %s: %w", symbol, err)
	}
	return &logo, nil
}

func (r *sqliteLogoRepository) Create(ctx context.Context, logo *model.Logo) error {
	// NamedExecContext uses the struct's `db:` tags to map fields to :named placeholders.
	result, err := r.db.NamedExecContext(ctx, `
		INSERT INTO logos (symbol, company_name, source, original_url, status)
		VALUES (:symbol, :company_name, :source, :original_url, :status)
	`, logo)
	if err != nil {
		return fmt.Errorf("creating logo: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	logo.ID = id
	return nil
}

func (r *sqliteLogoRepository) Update(ctx context.Context, logo *model.Logo) error {
	_, err := r.db.NamedExecContext(ctx, `
		UPDATE logos SET
			company_name = :company_name,
			source = :source,
			original_url = :original_url,
			has_xs = :has_xs,
			has_s = :has_s,
			has_m = :has_m,
			has_l = :has_l,
			has_xl = :has_xl,
			status = :status,
			error_message = :error_message,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = :id
	`, logo)
	if err != nil {
		return fmt.Errorf("updating logo: %w", err)
	}
	return nil
}

// SetSizeAvailable marks a specific size as available for a logo.
// This uses a technique to dynamically set a column — but since SQL column names
// can't be parameterized, we validate the column name via a map.
func (r *sqliteLogoRepository) SetSizeAvailable(ctx context.Context, symbol string, size model.LogoSize) error {
	// Map size to column name (prevents SQL injection since we control the values)
	columnMap := map[model.LogoSize]string{
		model.SizeXS: "has_xs",
		model.SizeS:  "has_s",
		model.SizeM:  "has_m",
		model.SizeL:  "has_l",
		model.SizeXL: "has_xl",
	}
	col, ok := columnMap[size]
	if !ok {
		return fmt.Errorf("invalid size: %s", size)
	}

	query := fmt.Sprintf("UPDATE logos SET %s = 1, updated_at = CURRENT_TIMESTAMP WHERE symbol = ?", col)
	_, err := r.db.ExecContext(ctx, query, symbol)
	if err != nil {
		return fmt.Errorf("setting size %s for %s: %w", size, symbol, err)
	}
	return nil
}

func (r *sqliteLogoRepository) SetStatus(ctx context.Context, symbol string, status model.LogoStatus, errMsg string) error {
	var err error
	if errMsg != "" {
		_, err = r.db.ExecContext(ctx,
			"UPDATE logos SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP WHERE symbol = ?",
			status, errMsg, symbol)
	} else {
		_, err = r.db.ExecContext(ctx,
			"UPDATE logos SET status = ?, error_message = NULL, updated_at = CURRENT_TIMESTAMP WHERE symbol = ?",
			status, symbol)
	}
	if err != nil {
		return fmt.Errorf("setting status for %s: %w", symbol, err)
	}
	return nil
}

func (r *sqliteLogoRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM logos")
	return count, err
}

func (r *sqliteLogoRepository) CountByStatus(ctx context.Context, status model.LogoStatus) (int64, error) {
	var count int64
	err := r.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM logos WHERE status = ?", status)
	return count, err
}

func (r *sqliteLogoRepository) ListPending(ctx context.Context, limit int) ([]model.Logo, error) {
	var logos []model.Logo
	err := r.db.SelectContext(ctx, &logos,
		"SELECT * FROM logos WHERE status = ? ORDER BY created_at ASC LIMIT ?",
		model.StatusPending, limit)
	if err != nil {
		return nil, fmt.Errorf("listing pending logos: %w", err)
	}
	return logos, nil
}

// LLMCallRepository handles persistence of LLM call tracking.
type LLMCallRepository interface {
	Create(ctx context.Context, call *model.LLMCall) error
	CountBySymbol(ctx context.Context, symbol string) (int64, error)
}

type sqliteLLMCallRepository struct {
	db *sqlx.DB
}

// NewLLMCallRepository creates a new SQLite-backed LLMCallRepository.
func NewLLMCallRepository(db *sqlx.DB) LLMCallRepository {
	return &sqliteLLMCallRepository{db: db}
}

func (r *sqliteLLMCallRepository) Create(ctx context.Context, call *model.LLMCall) error {
	result, err := r.db.NamedExecContext(ctx, `
		INSERT INTO llm_calls (symbol, provider, model, result_url, success, duration_ms)
		VALUES (:symbol, :provider, :model, :result_url, :success, :duration_ms)
	`, call)
	if err != nil {
		return fmt.Errorf("creating llm call record: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	call.ID = id
	return nil
}

func (r *sqliteLLMCallRepository) CountBySymbol(ctx context.Context, symbol string) (int64, error) {
	var count int64
	err := r.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM llm_calls WHERE symbol = ?", symbol)
	return count, err
}
