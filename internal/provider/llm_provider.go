package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/fleveque/logo-service/internal/llm"
	"github.com/fleveque/logo-service/internal/model"
	"github.com/fleveque/logo-service/internal/storage"
)

// LLMProvider uses an LLM (Claude or OpenAI) to find logos for tickers
// not covered by the GitHub repos. It:
// 1. Asks the LLM to search the web for the company's official logo
// 2. Downloads the image from the URL the LLM found
// 3. Returns the raw image bytes for processing
//
// Rate limited to prevent excessive API costs (~10 calls/minute).
// Tries providers in configured order — first success wins, failures fall through.
type LLMProvider struct {
	clients     []llm.Client // Ordered list: first is primary, rest are fallbacks
	limiter     *rate.Limiter
	llmCallRepo storage.LLMCallRepository
	httpClient  *http.Client
	logger      *zap.Logger
}

// NewLLMProvider creates a provider with an ordered list of LLM clients.
// The order is configurable via config.yaml: llm.provider_order: ["anthropic", "openai"]
// This means swapping provider priority is a config change, not a code change.
func NewLLMProvider(
	clients []llm.Client,
	ratePerMinute int,
	llmCallRepo storage.LLMCallRepository,
	logger *zap.Logger,
) *LLMProvider {
	// Convert rate per minute to rate per second for the limiter.
	// rate.Every returns a rate.Limit from a time interval between events.
	rps := rate.Every(time.Minute / time.Duration(ratePerMinute))

	return &LLMProvider{
		clients:     clients,
		limiter:     rate.NewLimiter(rps, 1), // burst of 1 — strict rate limiting
		llmCallRepo: llmCallRepo,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (p *LLMProvider) Name() string { return "llm" }

// GetLogo asks LLM providers (in configured order) to find a logo URL, then downloads it.
func (p *LLMProvider) GetLogo(ctx context.Context, symbol string) (*LogoResult, error) {
	if len(p.clients) == 0 {
		return nil, fmt.Errorf("no LLM providers configured")
	}

	var lastErr error

	// Try each provider in order. The order is set by config: llm.provider_order
	for i, client := range p.clients {
		// Rate limit — blocks until a token is available or context is cancelled.
		if err := p.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit wait: %w", err)
		}

		result, err := p.tryProvider(ctx, client, symbol)
		if err == nil {
			return result, nil
		}

		lastErr = err

		if i < len(p.clients)-1 {
			p.logger.Warn("LLM provider failed, trying next",
				zap.String("symbol", symbol),
				zap.String("provider", client.ProviderName()),
				zap.Error(err),
			)
		}
	}

	return nil, fmt.Errorf("all LLM providers failed for %s: %w", symbol, lastErr)
}

// BulkImport is not implemented for LLM — it's too expensive to search
// for every ticker. LLM is used on-demand for individual missing logos.
func (p *LLMProvider) BulkImport(_ context.Context, _ func(result *LogoResult) error) (*ImportStats, error) {
	return &ImportStats{}, fmt.Errorf("LLM provider does not support bulk import")
}

func (p *LLMProvider) tryProvider(ctx context.Context, client llm.Client, symbol string) (*LogoResult, error) {
	if client == nil {
		return nil, fmt.Errorf("LLM client not configured")
	}

	start := time.Now()

	searchResult, err := client.FindLogoURL(ctx, symbol, "")
	duration := time.Since(start).Milliseconds()

	// Record the LLM call for cost tracking
	p.recordCall(ctx, client, symbol, searchResult, err, duration)

	if err != nil {
		return nil, err
	}

	// Download the image from the URL the LLM found
	imageData, err := p.downloadImage(ctx, searchResult.LogoURL)
	if err != nil {
		return nil, fmt.Errorf("downloading logo from %s: %w", searchResult.LogoURL, err)
	}

	return &LogoResult{
		Symbol:      symbol,
		CompanyName: searchResult.CompanyName,
		ImageData:   imageData,
		Source:      fmt.Sprintf("llm:%s", client.ProviderName()),
		OriginalURL: searchResult.LogoURL,
	}, nil
}

func (p *LLMProvider) recordCall(ctx context.Context, client llm.Client, symbol string, result *llm.LogoSearchResult, callErr error, durationMs int64) {
	call := &model.LLMCall{
		Symbol:   symbol,
		Provider: client.ProviderName(),
		Model:    client.ModelName(),
		Success:  callErr == nil,
	}
	call.DurationMs = &durationMs
	if result != nil {
		call.ResultURL = &result.LogoURL
	}

	if err := p.llmCallRepo.Create(ctx, call); err != nil {
		p.logger.Error("recording LLM call", zap.Error(err))
	}
}

func (p *LLMProvider) downloadImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "logo-service/1.0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	return data, nil
}
