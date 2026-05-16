package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GeminiClient implements the Client interface using Google's Gemini API.
// We use raw HTTP rather than a vendored SDK to keep the dependency
// footprint small — the request shape is simple and unlikely to churn.
// Structured output is via `responseSchema` (Gemini's native equivalent
// of Anthropic/OpenAI tool calling), so we get clean JSON back without
// an agentic loop.
type GeminiClient struct {
	httpClient *http.Client
	apiKey     string
	model      string
}

const geminiAPIURLFormat = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s"

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
	prompt := buildPrompt(symbol, companyName)

	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]any{{"text": prompt}}},
		},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"responseSchema": map[string]any{
				"type": "OBJECT",
				"properties": map[string]any{
					"logo_url":     map[string]any{"type": "STRING"},
					"company_name": map[string]any{"type": "STRING"},
					"source":       map[string]any{"type": "STRING"},
					"confidence": map[string]any{
						"type": "STRING",
						"enum": []string{"high", "medium", "low"},
					},
				},
				"required": []string{"logo_url", "company_name", "confidence"},
			},
			"temperature":     0.2,
			"maxOutputTokens": 1024,
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

	var result submitLogoResult
	if err := json.Unmarshal([]byte(apiResp.Candidates[0].Content.Parts[0].Text), &result); err != nil {
		return nil, fmt.Errorf("parsing gemini structured output: %w", err)
	}

	if result.LogoURL == "" {
		return nil, fmt.Errorf("gemini did not find a logo URL for %s", symbol)
	}

	return &LogoSearchResult{
		LogoURL:     result.LogoURL,
		CompanyName: result.CompanyName,
		Source:      result.Source,
		Confidence:  result.Confidence,
	}, nil
}
