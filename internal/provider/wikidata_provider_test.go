package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// stubWikidata returns canned responses for the two wikidata.org endpoints
// and serves a tiny PNG body for the commons download.
func stubWikidata(t *testing.T, searchBody, claimsBody, imageBody string, imageStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("action") {
		case "wbsearchentities":
			_, _ = io.WriteString(w, searchBody)
		case "wbgetclaims":
			_, _ = io.WriteString(w, claimsBody)
		default:
			// Image download path — both Special:FilePath and the upload host alias here.
			w.WriteHeader(imageStatus)
			_, _ = io.WriteString(w, imageBody)
		}
	}))
}

func newWikidataProviderWithServer(srv *httptest.Server) *WikidataProvider {
	p := NewWikidataProvider(zap.NewNop())
	p.httpClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})
	return p
}

// roundTripperFunc — same trick used in the LLM tests.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestWikidataProvider_GetLogo_Success(t *testing.T) {
	srv := stubWikidata(t,
		`{"search":[{"id":"Q161140"}]}`,
		`{"claims":{"P154":[{"mainsnak":{"datavalue":{"value":"Diageo logo.svg"}}}]}}`,
		"PNGDATA", http.StatusOK,
	)
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	result, err := p.GetLogo(context.Background(), "DGE.L", "Diageo plc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.ImageData) != "PNGDATA" {
		t.Errorf("ImageData = %q", string(result.ImageData))
	}
	if !strings.Contains(result.OriginalURL, "Special:FilePath/") {
		t.Errorf("OriginalURL = %q", result.OriginalURL)
	}
	if !strings.Contains(result.OriginalURL, "Diageo") {
		t.Errorf("OriginalURL should contain the filename: %q", result.OriginalURL)
	}
	if result.Source != "wikidata:Q161140" {
		t.Errorf("Source = %q", result.Source)
	}
	if result.CompanyName != "Diageo plc" {
		t.Errorf("CompanyName = %q", result.CompanyName)
	}
}

func TestWikidataProvider_GetLogo_NoCompanyName(t *testing.T) {
	p := NewWikidataProvider(zap.NewNop())
	_, err := p.GetLogo(context.Background(), "AAPL", "")
	if err == nil {
		t.Fatal("expected error when company name is missing")
	}
}

func TestWikidataProvider_GetLogo_NoSearchResults(t *testing.T) {
	srv := stubWikidata(t, `{"search":[]}`, "", "", http.StatusOK)
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	_, err := p.GetLogo(context.Background(), "XYZ.ZZ", "Made Up Co.")
	if err == nil {
		t.Fatal("expected error when wikidata search returns no entity")
	}
}

func TestWikidataProvider_GetLogo_NoP154Claim(t *testing.T) {
	srv := stubWikidata(t,
		`{"search":[{"id":"Q999"}]}`,
		`{"claims":{}}`,
		"", http.StatusOK,
	)
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	_, err := p.GetLogo(context.Background(), "FOO", "Foo Corp")
	if err == nil {
		t.Fatal("expected error when entity has no P154 claim")
	}
}

func TestWikidataProvider_GetLogo_ImageDownloadFails(t *testing.T) {
	srv := stubWikidata(t,
		`{"search":[{"id":"Q161140"}]}`,
		`{"claims":{"P154":[{"mainsnak":{"datavalue":{"value":"Diageo logo.svg"}}}]}}`,
		"not found", http.StatusNotFound,
	)
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	_, err := p.GetLogo(context.Background(), "DGE.L", "Diageo plc")
	if err == nil {
		t.Fatal("expected error when Commons download 404s")
	}
}

func TestWikidataProvider_GetLogo_URLEncodesFilenameSpaces(t *testing.T) {
	srv := stubWikidata(t,
		`{"search":[{"id":"Q161140"}]}`,
		`{"claims":{"P154":[{"mainsnak":{"datavalue":{"value":"Diageo - logo (United Kingdom, 1997).svg"}}}]}}`,
		"PNG", http.StatusOK,
	)
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	result, err := p.GetLogo(context.Background(), "DGE.L", "Diageo plc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// PathEscape encodes spaces as %20 and parens/commas as-is. Just make sure no raw spaces leaked.
	if strings.Contains(result.OriginalURL, " ") {
		t.Errorf("OriginalURL must not contain unescaped spaces: %q", result.OriginalURL)
	}
}

func TestWikidataProvider_BulkImport_NotSupported(t *testing.T) {
	p := NewWikidataProvider(zap.NewNop())
	_, err := p.BulkImport(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from BulkImport")
	}
}
