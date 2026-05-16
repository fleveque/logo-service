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
	// Wikimedia rasterizes SVGs server-side when you append `?width=`. Asking
	// for ~512px gives us a PNG large enough to downsize to our biggest output
	// (xl=256px) without quality loss, but small enough to download fast. This
	// means our image processor (libvips, compiled without rsvg on Alpine)
	// only ever sees PNGs from this provider.
	commonsThumbWidth = 512
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

	// Wikimedia gives us a server-rasterized PNG when the file is an SVG and we
	// append `?width=`. For non-SVG files the query param is ignored and we get
	// the original. Either way, libvips downstream just sees a raster image.
	logoURL := fmt.Sprintf("%s%s?width=%d", commonsFile, url.PathEscape(filename), commonsThumbWidth)
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

// Lowercased corporate-form tokens we look for inside the company name. Yahoo
// often hands us not just the company name but the full security descriptor
// ("DIAGEO PLC ORD 28 101/108P"), so we can't rely on the form being a trailing
// suffix — we find it as a word and truncate everything after it.
var corporateForms = []string{
	"plc", "p.l.c.",
	"inc", "inc.",
	"sa", "s.a", "s.a.",
	"ltd", "ltd.", "limited",
	"corp", "corp.", "corporation",
	"ag",
	"gmbh",
	"nv", "n.v", "n.v.",
	"se", "s.e.",
	"co", "co.",
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// companyNameVariants returns search-friendly variants of a raw company name
// in decreasing order of fidelity, e.g. for "DIAGEO PLC ORD 28 101/108P":
//
//	["DIAGEO PLC ORD 28 101/108P", "DIAGEO PLC", "DIAGEO"]
//
// Wikidata's fuzzy search picks up the right entity from the bare name even
// when the descriptive cruft drowns it out in the as-is form.
func companyNameVariants(name string) []string {
	normalized := normalizeWhitespace(name)
	variants := []string{normalized}

	words := strings.Fields(normalized)
	for i, word := range words {
		token := strings.ToLower(strings.TrimRight(word, ".,"))
		if !isCorporateForm(token) {
			continue
		}

		// "<name…> <CorpForm>" — drop trailing security-descriptor cruft.
		withForm := strings.Join(words[:i+1], " ")
		if withForm != normalized {
			variants = append(variants, withForm)
		}

		// "<name…>" — drop the corporate form too. Also trim trailing
		// punctuation that often sits between the name and the form,
		// e.g. "REPSOL," → "REPSOL".
		if i > 0 {
			bare := strings.TrimRight(strings.Join(words[:i], " "), ",")
			if bare != "" && bare != withForm {
				variants = append(variants, bare)
			}
		}
		break // first match wins; further tokens are inside the descriptor
	}

	return variants
}

func isCorporateForm(token string) bool {
	for _, f := range corporateForms {
		if token == f {
			return true
		}
	}
	return false
}

// searchEntity tries each variant of the company name in order, stopping at
// the first hit. At most 3 queries per call (raw → with-form → bare), all
// against Wikidata's free API.
func (w *WikidataProvider) searchEntity(ctx context.Context, name string) (string, error) {
	variants := companyNameVariants(name)

	for _, v := range variants {
		if v == "" {
			continue
		}
		if id, err := w.searchOnce(ctx, v); err == nil {
			return id, nil
		}
	}

	return "", fmt.Errorf("no wikidata entity for any variant of %q (tried %d: %v)",
		variants[0], len(variants), variants)
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
