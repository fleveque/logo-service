package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// GeminiClient implements the Client interface using Google's Gemini API
// with Google Search grounding enabled. Without grounding the model
// happily fabricates plausible-looking URLs (e.g. invented Wikipedia
// Commons hash prefixes) which then 404 on download. Grounding forces
// the response to cite URLs the model actually saw via search.
//
// Note: Gemini's API does not allow `responseSchema` together with the
// `google_search` tool — they're mutually exclusive. We therefore parse
// a tagged `<LOGO_URL>...</LOGO_URL>` wrapper out of the free-text
// response rather than relying on structured output.
type GeminiClient struct {
	httpClient *http.Client
	apiKey     string
	model      string
}

const geminiAPIURLFormat = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s"

var (
	logoURLRegex = regexp.MustCompile(`<LOGO_URL>\s*(\S+?)\s*</LOGO_URL>`)
	sourceRegex  = regexp.MustCompile(`<SOURCE>\s*(\S+?)\s*</SOURCE>`)
)

// NewGeminiClient creates a Gemini-powered logo finder.
func NewGeminiClient(apiKey, model string) *GeminiClient {
	return &GeminiClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		apiKey:     apiKey,
		model:      model,
	}
}

func (g *GeminiClient) ProviderName() string { return "gemini" }
func (g *GeminiClient) ModelName() string    { return g.model }

func (g *GeminiClient) FindLogoURL(ctx context.Context, symbol, companyName string) (*LogoSearchResult, error) {
	prompt := buildGeminiGroundedPrompt(symbol, companyName)

	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]any{{"text": prompt}}},
		},
		"tools": []map[string]any{
			{"google_search": map[string]any{}},
		},
		"generationConfig": map[string]any{
			"temperature":     0.2,
			"maxOutputTokens": 2048,
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling gemini request: %w", err)
	}

	url := fmt.Sprintf(geminiAPIURLFormat, g.model, g.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading gemini response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decoding gemini response envelope: %w", err)
	}

	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini returned no content for %s", symbol)
	}

	// Concatenate every text part — grounded responses can split text across multiple
	// parts when interleaved with search results.
	var sb strings.Builder
	for _, part := range apiResp.Candidates[0].Content.Parts {
		sb.WriteString(part.Text)
	}
	text := sb.String()

	logoURL := extractTagged(logoURLRegex, text)
	if logoURL == "" {
		return nil, fmt.Errorf("gemini did not return a tagged logo URL for %s; raw response: %q", symbol, truncate(text, 200))
	}
	source := extractTagged(sourceRegex, text)

	return &LogoSearchResult{
		LogoURL:     logoURL,
		CompanyName: companyName,
		Source:      source,
		Confidence:  "grounded",
	}, nil
}

func extractTagged(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// buildGeminiGroundedPrompt is Gemini-specific because the contract is different
// from the JSON-schema variant used by Anthropic / OpenAI clients: we ask for a
// tagged free-text response since `google_search` and `responseSchema` can't
// coexist in the same Gemini call.
func buildGeminiGroundedPrompt(symbol, companyName string) string {
	hint := ""
	if companyName != "" {
		hint = fmt.Sprintf(" (company name: %s)", companyName)
	}

	return fmt.Sprintf(`Find the official company logo for stock ticker "%s"%s.

Use Google Search to locate a DIRECT image URL for the company's official logo. Only return a URL you actually saw in the search results — do not guess or construct URLs from patterns you remember.

Prefer (in order):
1. Wikipedia / Wikimedia Commons file pages — copy the actual file URL from the page
2. The company's own website (look for /favicon.png, brand assets, press kit pages)
3. Reputable financial data sites (Yahoo Finance, Google Finance)

Requirements:
- Must be a DIRECT link to an image file (URL ends in .png, .svg, .jpg, .jpeg, or .webp)
- Must be publicly accessible (no auth, no paywall)
- Must be the company's primary logo, not a product or sub-brand variant

Output format: after any reasoning, end your response with these two tags on their own lines:

<LOGO_URL>the direct image URL you found</LOGO_URL>
<SOURCE>the page where you found it</SOURCE>

If you cannot find a logo URL that meets all the requirements, output:

<LOGO_URL></LOGO_URL>`, symbol, hint)
}
