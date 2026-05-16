package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// WikidataProvider resolves a company name to its official logo via Wikidata.
// Deterministic — no LLM, no hallucination risk. Inserted between the GitHub
// repos (US-only) and the LLM layer so non-US tickers with a Wikipedia page
// (Diageo, Repsol, Iberdrola, …) get a real Commons file URL without needing
// the slow + occasionally-wrong web-search path.
//
// Pipeline per call:
//
//	1. Wikidata `wbsearchentities` for `companyName` → first result's entity ID (Q…)
//	2. `wbgetclaims` for property P154 (logo image) → the Commons filename
//	3. Download via `commons.wikimedia.org/wiki/Special:FilePath/<filename>`
//	   which redirects to the actual upload.wikimedia.org URL — so we don't
//	   have to compute the MD5-derived hash prefix ourselves.
type WikidataProvider struct {
	httpClient *http.Client
	logger     *zap.Logger
}

const (
	wikidataAPI = "https://www.wikidata.org/w/api.php"
	commonsFile = "https://commons.wikimedia.org/wiki/Special:FilePath/"
)

func NewWikidataProvider(logger *zap.Logger) *WikidataProvider {
	return &WikidataProvider{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		logger:     logger,
	}
}

func (w *WikidataProvider) Name() string { return "wikidata" }

// GetLogo runs the full Wikidata→Commons pipeline. Returns an error (and the
// caller falls through to LLM) if any step fails, including a missing
// companyName which we treat as "Wikidata not usable for this request".
func (w *WikidataProvider) GetLogo(ctx context.Context, symbol, companyName string) (*LogoResult, error) {
	if strings.TrimSpace(companyName) == "" {
		return nil, fmt.Errorf("wikidata lookup requires company_name")
	}

	entityID, err := w.searchEntity(ctx, companyName)
	if err != nil {
		return nil, fmt.Errorf("wikidata search: %w", err)
	}

	filename, err := w.getLogoFilename(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("wikidata P154 for %s: %w", entityID, err)
	}

	logoURL := commonsFile + url.PathEscape(filename)
	data, err := w.downloadImage(ctx, logoURL)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", logoURL, err)
	}

	w.logger.Debug("wikidata logo resolved",
		zap.String("symbol", symbol),
		zap.String("entity", entityID),
		zap.String("filename", filename),
	)

	return &LogoResult{
		Symbol:      symbol,
		CompanyName: companyName,
		ImageData:   data,
		Source:      "wikidata:" + entityID,
		OriginalURL: logoURL,
	}, nil
}

// BulkImport is intentionally a no-op: Wikidata is for on-demand misses,
// not bulk seeding (the GitHub repos cover that use case better).
func (w *WikidataProvider) BulkImport(_ context.Context, _ func(*LogoResult) error) (*ImportStats, error) {
	return &ImportStats{}, fmt.Errorf("wikidata provider does not support bulk import")
}

type wbSearchResponse struct {
	Search []struct {
		ID string `json:"id"`
	} `json:"search"`
}

// Common corporate suffixes — we try these *after* a normalized search fails, so
// names like "REPSOL,  S.A." (caps, double whitespace, suffix) collapse to a
// query Wikidata can match. Order matters: longer suffixes first so we don't
// accidentally peel off "Inc" when the real suffix is "Inc.".
var corporateSuffixes = []string{
	", S.A.", " S.A.", ", S.A", " S.A",
	", Inc.", " Inc.", ", Inc", " Inc",
	", N.V.", " N.V.", ", NV", " NV",
	" Corporation", " Corp.", " Corp",
	" Limited", " Ltd.", " Ltd",
	" plc", " PLC", " p.l.c.",
	" GmbH", " AG", " SE", " S.E.",
	" Co., Ltd.", " Co.", " & Co.",
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func stripCorporateSuffix(name string) string {
	for _, suffix := range corporateSuffixes {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSpace(strings.TrimSuffix(name, suffix))
		}
	}
	return name
}

// searchEntity tries the normalized name first; if Wikidata returns no hits,
// it falls back to a suffix-stripped variant. Two queries worst case — cheap
// against Wikidata's free API and dramatically improves coverage for names
// pulled from Yahoo (which often arrive as "REPSOL,  S.A." or "Diageo plc").
func (w *WikidataProvider) searchEntity(ctx context.Context, name string) (string, error) {
	normalized := normalizeWhitespace(name)
	if id, err := w.searchOnce(ctx, normalized); err == nil {
		return id, nil
	}

	stripped := stripCorporateSuffix(normalized)
	if stripped == "" || stripped == normalized {
		return "", fmt.Errorf("no wikidata entity for %q", normalized)
	}

	id, err := w.searchOnce(ctx, stripped)
	if err != nil {
		return "", fmt.Errorf("no wikidata entity for %q or %q", normalized, stripped)
	}
	return id, nil
}

func (w *WikidataProvider) searchOnce(ctx context.Context, query string) (string, error) {
	q := url.Values{
		"action":   {"wbsearchentities"},
		"search":   {query},
		"language": {"en"},
		"limit":    {"1"},
		"format":   {"json"},
	}
	body, err := w.httpGet(ctx, wikidataAPI+"?"+q.Encode())
	if err != nil {
		return "", err
	}

	var resp wbSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decoding search response: %w", err)
	}
	if len(resp.Search) == 0 {
		return "", fmt.Errorf("no entity")
	}
	return resp.Search[0].ID, nil
}

type wbClaimsResponse struct {
	Claims map[string][]struct {
		Mainsnak struct {
			DataValue struct {
				Value string `json:"value"`
			} `json:"datavalue"`
		} `json:"mainsnak"`
	} `json:"claims"`
}

func (w *WikidataProvider) getLogoFilename(ctx context.Context, entityID string) (string, error) {
	q := url.Values{
		"action":   {"wbgetclaims"},
		"entity":   {entityID},
		"property": {"P154"},
		"format":   {"json"},
	}
	body, err := w.httpGet(ctx, wikidataAPI+"?"+q.Encode())
	if err != nil {
		return "", err
	}

	var resp wbClaimsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decoding claims response: %w", err)
	}

	claims := resp.Claims["P154"]
	if len(claims) == 0 {
		return "", fmt.Errorf("entity %s has no P154 (logo image) claim", entityID)
	}
	filename := strings.TrimSpace(claims[0].Mainsnak.DataValue.Value)
	if filename == "" {
		return "", fmt.Errorf("entity %s has empty P154 value", entityID)
	}
	return filename, nil
}

func (w *WikidataProvider) httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "logo-service/1.0 (https://github.com/fleveque/logo-service)")
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (w *WikidataProvider) downloadImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "logo-service/1.0 (https://github.com/fleveque/logo-service)")
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}
