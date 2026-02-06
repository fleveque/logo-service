// Go testing basics:
// - Test files must end with _test.go (they're excluded from production builds)
// - Test functions must start with Test and take *testing.T
// - Run with: go test ./internal/storage/ -v
// - t.Fatal stops the test immediately; t.Error continues to find more failures
package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fleveque/logo-service/internal/model"
)

// setupTestDB creates a temporary SQLite database for testing.
// Go's testing.T has a TempDir() method that creates a temp directory
// automatically cleaned up after the test — no manual teardown needed.
func setupTestDB(t *testing.T) *testDeps {
	t.Helper() // marks this as a helper so error line numbers point to the caller

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewDatabase(dbPath)
	if err != nil {
		t.Fatalf("creating test database: %v", err)
	}

	// t.Cleanup registers a function to run when the test finishes.
	// Similar to defer, but scoped to the test lifecycle.
	t.Cleanup(func() {
		db.Close()
		os.Remove(dbPath)
	})

	return &testDeps{
		logoRepo:    NewLogoRepository(db),
		llmCallRepo: NewLLMCallRepository(db),
	}
}

type testDeps struct {
	logoRepo    LogoRepository
	llmCallRepo LLMCallRepository
}

func TestLogoRepository_CreateAndGet(t *testing.T) {
	deps := setupTestDB(t)
	ctx := context.Background()

	// Create a logo
	logo := &model.Logo{
		Symbol:      "AAPL",
		CompanyName: "Apple Inc.",
		Source:      "github",
		OriginalURL: "https://example.com/aapl.png",
		Status:      model.StatusPending,
	}

	err := deps.logoRepo.Create(ctx, logo)
	if err != nil {
		t.Fatalf("creating logo: %v", err)
	}

	// In Go, after Create the ID should be populated (we set it in the repo)
	if logo.ID == 0 {
		t.Error("expected logo ID to be set after create")
	}

	// Retrieve it
	got, err := deps.logoRepo.GetBySymbol(ctx, "AAPL")
	if err != nil {
		t.Fatalf("getting logo: %v", err)
	}

	if got.Symbol != "AAPL" {
		t.Errorf("expected symbol AAPL, got %s", got.Symbol)
	}
	if got.CompanyName != "Apple Inc." {
		t.Errorf("expected company name 'Apple Inc.', got %s", got.CompanyName)
	}
	if got.Status != model.StatusPending {
		t.Errorf("expected status pending, got %s", got.Status)
	}
}

func TestLogoRepository_GetBySymbol_NotFound(t *testing.T) {
	deps := setupTestDB(t)
	ctx := context.Background()

	_, err := deps.logoRepo.GetBySymbol(ctx, "DOESNOTEXIST")
	if err == nil {
		t.Fatal("expected error for non-existent symbol, got nil")
	}
	// errors.Is checks the error chain — %w wrapping preserves the original error
	// so you can match against sentinel values like ErrNotFound.
}

func TestLogoRepository_Update(t *testing.T) {
	deps := setupTestDB(t)
	ctx := context.Background()

	logo := &model.Logo{
		Symbol: "MSFT",
		Source: "github",
		Status: model.StatusPending,
	}
	if err := deps.logoRepo.Create(ctx, logo); err != nil {
		t.Fatalf("creating logo: %v", err)
	}

	// Update it
	logo.CompanyName = "Microsoft Corporation"
	logo.Status = model.StatusProcessed
	logo.HasM = true
	if err := deps.logoRepo.Update(ctx, logo); err != nil {
		t.Fatalf("updating logo: %v", err)
	}

	// Verify the update
	got, err := deps.logoRepo.GetBySymbol(ctx, "MSFT")
	if err != nil {
		t.Fatalf("getting logo: %v", err)
	}
	if got.CompanyName != "Microsoft Corporation" {
		t.Errorf("expected company name 'Microsoft Corporation', got %s", got.CompanyName)
	}
	if got.Status != model.StatusProcessed {
		t.Errorf("expected status processed, got %s", got.Status)
	}
	if !got.HasM {
		t.Error("expected has_m to be true")
	}
}

func TestLogoRepository_SetSizeAvailable(t *testing.T) {
	deps := setupTestDB(t)
	ctx := context.Background()

	logo := &model.Logo{
		Symbol: "GOOG",
		Source: "llm",
		Status: model.StatusPending,
	}
	if err := deps.logoRepo.Create(ctx, logo); err != nil {
		t.Fatalf("creating logo: %v", err)
	}

	// Set individual sizes
	for _, size := range model.AllSizes {
		if err := deps.logoRepo.SetSizeAvailable(ctx, "GOOG", size); err != nil {
			t.Fatalf("setting size %s: %v", size, err)
		}
	}

	got, err := deps.logoRepo.GetBySymbol(ctx, "GOOG")
	if err != nil {
		t.Fatalf("getting logo: %v", err)
	}

	// All sizes should be true
	for _, size := range model.AllSizes {
		if !got.HasSize(size) {
			t.Errorf("expected size %s to be available", size)
		}
	}
}

func TestLogoRepository_CountAndListPending(t *testing.T) {
	deps := setupTestDB(t)
	ctx := context.Background()

	// Create several logos with different statuses
	symbols := []struct {
		symbol string
		status model.LogoStatus
	}{
		{"AAPL", model.StatusProcessed},
		{"MSFT", model.StatusPending},
		{"GOOG", model.StatusPending},
		{"TSLA", model.StatusFailed},
	}

	for _, s := range symbols {
		logo := &model.Logo{Symbol: s.symbol, Source: "test", Status: s.status}
		if err := deps.logoRepo.Create(ctx, logo); err != nil {
			t.Fatalf("creating logo %s: %v", s.symbol, err)
		}
	}

	// Total count
	count, err := deps.logoRepo.Count(ctx)
	if err != nil {
		t.Fatalf("counting logos: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 logos, got %d", count)
	}

	// Count by status
	pendingCount, err := deps.logoRepo.CountByStatus(ctx, model.StatusPending)
	if err != nil {
		t.Fatalf("counting pending logos: %v", err)
	}
	if pendingCount != 2 {
		t.Errorf("expected 2 pending logos, got %d", pendingCount)
	}

	// List pending
	pending, err := deps.logoRepo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("listing pending logos: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending logos, got %d", len(pending))
	}
}

func TestLLMCallRepository_Create(t *testing.T) {
	deps := setupTestDB(t)
	ctx := context.Background()

	resultURL := "https://example.com/logo.png"
	duration := int64(1500)

	call := &model.LLMCall{
		Symbol:     "AAPL",
		Provider:   "anthropic",
		Model:      "claude-sonnet-4-5-20250929",
		ResultURL:  &resultURL,
		Success:    true,
		DurationMs: &duration,
	}

	if err := deps.llmCallRepo.Create(ctx, call); err != nil {
		t.Fatalf("creating llm call: %v", err)
	}

	if call.ID == 0 {
		t.Error("expected llm call ID to be set after create")
	}

	// Count calls for the symbol
	count, err := deps.llmCallRepo.CountBySymbol(ctx, "AAPL")
	if err != nil {
		t.Fatalf("counting llm calls: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 llm call, got %d", count)
	}
}
