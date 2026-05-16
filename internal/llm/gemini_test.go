package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// roundTripperFunc lets us stub the HTTP layer without a third-party mock.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func newGeminiTestServer(t *testing.T, status int, responseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, responseBody)
	}))
}

func newGeminiClientWithServer(srv *httptest.Server) *GeminiClient {
	client := &GeminiClient{
		httpClient: srv.Client(),
		apiKey:     "test-key",
		model:      "gemini-2.5-flash",
	}
	client.httpClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})
	return client
}

func TestGeminiClient_FindLogoURL_Success(t *testing.T) {
	// Grounded text response with the tagged wrapper.
	text := "I searched and found Repsol's logo on Wikipedia.\n\n<LOGO_URL>https://upload.wikimedia.org/repsol.png</LOGO_URL>\n<SOURCE>https://en.wikipedia.org/wiki/Repsol</SOURCE>"
	payload, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{"content": map[string]any{
				"parts": []map[string]any{{"text": text}},
			}},
		},
	})
	srv := newGeminiTestServer(t, http.StatusOK, string(payload))
	defer srv.Close()

	client := newGeminiClientWithServer(srv)

	result, err := client.FindLogoURL(context.Background(), "REP.MC", "Repsol, S.A.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LogoURL != "https://upload.wikimedia.org/repsol.png" {
		t.Errorf("LogoURL = %q", result.LogoURL)
	}
	if result.CompanyName != "Repsol, S.A." {
		t.Errorf("CompanyName = %q", result.CompanyName)
	}
	if result.Source != "https://en.wikipedia.org/wiki/Repsol" {
		t.Errorf("Source = %q", result.Source)
	}
}

func TestGeminiClient_FindLogoURL_MultiplePartsConcatenated(t *testing.T) {
	// Grounded responses often split text across multiple parts.
	payload, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{"content": map[string]any{
				"parts": []map[string]any{
					{"text": "Searching..."},
					{"text": "Found it: <LOGO_URL>https://example.com/x.svg</LOGO_URL>"},
				},
			}},
		},
	})
	srv := newGeminiTestServer(t, http.StatusOK, string(payload))
	defer srv.Close()

	client := newGeminiClientWithServer(srv)

	result, err := client.FindLogoURL(context.Background(), "AAPL", "Apple")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LogoURL != "https://example.com/x.svg" {
		t.Errorf("LogoURL = %q", result.LogoURL)
	}
}

func TestGeminiClient_FindLogoURL_EmptyTag(t *testing.T) {
	// Model returned the "couldn't find" sentinel.
	payload, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{"content": map[string]any{
				"parts": []map[string]any{{"text": "No reliable logo found.\n<LOGO_URL></LOGO_URL>"}},
			}},
		},
	})
	srv := newGeminiTestServer(t, http.StatusOK, string(payload))
	defer srv.Close()

	client := newGeminiClientWithServer(srv)
	_, err := client.FindLogoURL(context.Background(), "XYZ", "")
	if err == nil {
		t.Fatal("expected error for empty <LOGO_URL>, got nil")
	}
}

func TestGeminiClient_FindLogoURL_MissingTag(t *testing.T) {
	// Model went off-script and didn't emit the wrapper at all.
	payload, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{"content": map[string]any{
				"parts": []map[string]any{{"text": "Sure! The logo is at https://example.com/x.png"}},
			}},
		},
	})
	srv := newGeminiTestServer(t, http.StatusOK, string(payload))
	defer srv.Close()

	client := newGeminiClientWithServer(srv)
	_, err := client.FindLogoURL(context.Background(), "ABC", "")
	if err == nil {
		t.Fatal("expected error for missing <LOGO_URL> tag, got nil")
	}
}

func TestGeminiClient_FindLogoURL_HTTPError(t *testing.T) {
	srv := newGeminiTestServer(t, http.StatusInternalServerError, `{"error":"boom"}`)
	defer srv.Close()

	client := newGeminiClientWithServer(srv)
	_, err := client.FindLogoURL(context.Background(), "AAPL", "Apple")
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestBuildGeminiGroundedPrompt_IncludesCompanyName(t *testing.T) {
	prompt := buildGeminiGroundedPrompt("REP.MC", "Repsol, S.A.")
	if !strings.Contains(prompt, "Repsol, S.A.") {
		t.Errorf("prompt missing company name hint: %s", prompt)
	}
	if !strings.Contains(prompt, "<LOGO_URL>") {
		t.Errorf("prompt missing tag spec: %s", prompt)
	}
}

func TestBuildGeminiGroundedPrompt_OmitsHintWhenNoName(t *testing.T) {
	prompt := buildGeminiGroundedPrompt("AAPL", "")
	if strings.Contains(prompt, "company name:") {
		t.Errorf("prompt should not include hint when name empty: %s", prompt)
	}
}

// Request body must include the google_search tool and NOT include responseSchema
// (they're mutually exclusive in Gemini).
func TestGeminiClient_RequestShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"<LOGO_URL>https://x/y.png</LOGO_URL>"}]}}]}`)
	}))
	defer srv.Close()

	client := newGeminiClientWithServer(srv)
	if _, err := client.FindLogoURL(context.Background(), "AAPL", "Apple"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools, ok := captured["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatal("request missing tools array")
	}
	tool, _ := tools[0].(map[string]any)
	if _, hasSearch := tool["google_search"]; !hasSearch {
		t.Errorf("first tool is not google_search: %v", tool)
	}

	if gc, ok := captured["generationConfig"].(map[string]any); ok {
		if _, hasSchema := gc["responseSchema"]; hasSchema {
			t.Error("generationConfig must not include responseSchema (incompatible with google_search)")
		}
		if _, hasMime := gc["responseMimeType"]; hasMime {
			t.Error("generationConfig must not include responseMimeType (incompatible with google_search)")
		}
	}
}
