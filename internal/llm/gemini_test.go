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

func TestGeminiClient_FindLogoURL_Success(t *testing.T) {
	payload := `{"candidates":[{"content":{"parts":[{"text":"{\"logo_url\":\"https://example.com/repsol.png\",\"company_name\":\"Repsol, S.A.\",\"source\":\"wikipedia.org\",\"confidence\":\"high\"}"}]}}]}`
	srv := newGeminiTestServer(t, http.StatusOK, payload)
	defer srv.Close()

	client := &GeminiClient{
		httpClient: srv.Client(),
		apiKey:     "test-key",
		model:      "gemini-2.5-flash",
	}
	// Override the URL by hijacking the transport (Gemini URL is hardcoded).
	// We swap the entire HTTP client so the URL doesn't matter — the test server's
	// handler answers any request.
	client.httpClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// Forward every request to our test server, preserving body so request shape can be inspected.
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})

	result, err := client.FindLogoURL(context.Background(), "REP.MC", "Repsol, S.A.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LogoURL != "https://example.com/repsol.png" {
		t.Errorf("LogoURL = %q, want https://example.com/repsol.png", result.LogoURL)
	}
	if result.CompanyName != "Repsol, S.A." {
		t.Errorf("CompanyName = %q, want Repsol, S.A.", result.CompanyName)
	}
	if result.Confidence != "high" {
		t.Errorf("Confidence = %q, want high", result.Confidence)
	}
}

func TestGeminiClient_FindLogoURL_EmptyURL(t *testing.T) {
	payload := `{"candidates":[{"content":{"parts":[{"text":"{\"logo_url\":\"\",\"company_name\":\"Unknown\",\"confidence\":\"low\"}"}]}}]}`
	srv := newGeminiTestServer(t, http.StatusOK, payload)
	defer srv.Close()

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

	_, err := client.FindLogoURL(context.Background(), "XYZ", "")
	if err == nil {
		t.Fatal("expected error for empty logo URL, got nil")
	}
}

func TestGeminiClient_FindLogoURL_HTTPError(t *testing.T) {
	srv := newGeminiTestServer(t, http.StatusInternalServerError, `{"error":"boom"}`)
	defer srv.Close()

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

	_, err := client.FindLogoURL(context.Background(), "AAPL", "Apple")
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestBuildPrompt_IncludesCompanyName(t *testing.T) {
	prompt := buildPrompt("REP.MC", "Repsol, S.A.")
	if !strings.Contains(prompt, "Repsol, S.A.") {
		t.Errorf("prompt missing company name hint: %s", prompt)
	}
}

func TestBuildPrompt_OmitsHintWhenNoName(t *testing.T) {
	prompt := buildPrompt("AAPL", "")
	if strings.Contains(prompt, "company name:") {
		t.Errorf("prompt should not include hint when name empty: %s", prompt)
	}
}

// Ensures the request body shape is what Gemini expects (responseSchema present).
func TestGeminiClient_RequestShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"{\"logo_url\":\"https://x/y.png\",\"company_name\":\"X\",\"confidence\":\"high\"}"}]}}]}`)
	}))
	defer srv.Close()

	client := &GeminiClient{
		httpClient: srv.Client(),
		apiKey:     "k",
		model:      "gemini-2.5-flash",
	}
	client.httpClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})

	if _, err := client.FindLogoURL(context.Background(), "AAPL", "Apple"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gc, ok := captured["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("missing generationConfig in request")
	}
	if gc["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v, want application/json", gc["responseMimeType"])
	}
	if _, ok := gc["responseSchema"]; !ok {
		t.Error("responseSchema missing from request")
	}
}
