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
	if !strings.Contains(result.OriginalURL, "width=") {
		t.Errorf("OriginalURL should ask Wikimedia to rasterize SVG → PNG via width=: %q", result.OriginalURL)
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

func TestNormalizeWhitespace(t *testing.T) {
	cases := map[string]string{
		"REPSOL,  S.A.":      "REPSOL, S.A.",
		"  Diageo plc  ":     "Diageo plc",
		"Apple\tInc.":        "Apple Inc.",
		"Multi\n  Line\tCo.": "Multi Line Co.",
	}
	for input, want := range cases {
		if got := normalizeWhitespace(input); got != want {
			t.Errorf("normalizeWhitespace(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCompanyNameVariants(t *testing.T) {
	cases := map[string][]string{
		// Whitespace + trailing corp form: variants = original, then bare name.
		"REPSOL,  S.A.": {"REPSOL, S.A.", "REPSOL"},
		// Trailing plc: original then bare.
		"Diageo plc": {"Diageo plc", "Diageo"},
		// Yahoo descriptor noise after the corp form — must truncate to "with form"
		// AND "bare".
		"DIAGEO PLC ORD 28 101/108P": {"DIAGEO PLC ORD 28 101/108P", "DIAGEO PLC", "DIAGEO"},
		// Multi-word name with trailing form: same shape.
		"Toyota Motor Corporation": {"Toyota Motor Corporation", "Toyota Motor"},
		// Inc.
		"Apple Inc.": {"Apple Inc.", "Apple"},
		// No corp form anywhere: just the normalized name.
		"Berkshire Hathaway": {"Berkshire Hathaway"},
	}
	for input, want := range cases {
		got := companyNameVariants(input)
		if !equalSlices(got, want) {
			t.Errorf("companyNameVariants(%q) = %v, want %v", input, got, want)
		}
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Whitespace-only difference: the as-is search succeeds, no fallback needed.
func TestWikidataProvider_GetLogo_RecoversFromDoubleSpace(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("action") {
		case "wbsearchentities":
			calls++
			search := q.Get("search")
			// Whitespace-collapsed input must be the FIRST query — and we expect
			// it to hit on the first try (single query).
			if search != "REPSOL, S.A." {
				t.Errorf("call %d: expected first search to be whitespace-collapsed, got %q", calls, search)
			}
			_, _ = io.WriteString(w, `{"search":[{"id":"Q174747"}]}`)
		case "wbgetclaims":
			_, _ = io.WriteString(w, `{"claims":{"P154":[{"mainsnak":{"datavalue":{"value":"Repsol logo.svg"}}}]}}`)
		default:
			_, _ = io.WriteString(w, "PNGDATA")
		}
	}))
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	result, err := p.GetLogo(context.Background(), "REP.MC", "REPSOL,  S.A.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != "wikidata:Q174747" {
		t.Errorf("Source = %q", result.Source)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 search call (whitespace-only fix), got %d", calls)
	}
}

// First (normalized) search misses, bare-name retry hits.
func TestWikidataProvider_GetLogo_RecoversByStrippingCorpForm(t *testing.T) {
	searches := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("action") {
		case "wbsearchentities":
			search := q.Get("search")
			searches = append(searches, search)
			// First search (as-is): miss. Second (bare): hit.
			if len(searches) == 1 {
				_, _ = io.WriteString(w, `{"search":[]}`)
			} else {
				_, _ = io.WriteString(w, `{"search":[{"id":"Q200147"}]}`)
			}
		case "wbgetclaims":
			_, _ = io.WriteString(w, `{"claims":{"P154":[{"mainsnak":{"datavalue":{"value":"Some logo.svg"}}}]}}`)
		default:
			_, _ = io.WriteString(w, "PNGDATA")
		}
	}))
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	result, err := p.GetLogo(context.Background(), "FOO", "ObscureName Inc.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != "wikidata:Q200147" {
		t.Errorf("Source = %q", result.Source)
	}
	if len(searches) != 2 {
		t.Fatalf("expected 2 search calls (as-is + bare), got %d: %v", len(searches), searches)
	}
	if !strings.Contains(searches[0], "Inc.") {
		t.Errorf("first search should keep the corp form: %q", searches[0])
	}
	if strings.Contains(searches[1], "Inc.") {
		t.Errorf("second search should drop the corp form: %q", searches[1])
	}
}

// Yahoo-style descriptor noise — variants must include the truncated form so
// Wikidata can find Diageo when handed "DIAGEO PLC ORD 28 101/108P".
func TestWikidataProvider_GetLogo_HandlesYahooSecurityDescriptor(t *testing.T) {
	searches := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("action") {
		case "wbsearchentities":
			search := q.Get("search")
			searches = append(searches, search)
			// First two miss (raw + truncated-with-form), third hits ("DIAGEO").
			if len(searches) <= 2 {
				_, _ = io.WriteString(w, `{"search":[]}`)
			} else {
				_, _ = io.WriteString(w, `{"search":[{"id":"Q161140"}]}`)
			}
		case "wbgetclaims":
			_, _ = io.WriteString(w, `{"claims":{"P154":[{"mainsnak":{"datavalue":{"value":"Diageo logo.svg"}}}]}}`)
		default:
			_, _ = io.WriteString(w, "PNGDATA")
		}
	}))
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	result, err := p.GetLogo(context.Background(), "DGE.L", "DIAGEO PLC ORD 28 101/108P")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != "wikidata:Q161140" {
		t.Errorf("Source = %q", result.Source)
	}
	if len(searches) != 3 {
		t.Fatalf("expected 3 search calls (raw → with-form → bare), got %d: %v", len(searches), searches)
	}
	if searches[len(searches)-1] != "DIAGEO" {
		t.Errorf("final search should be the bare name, got %q", searches[len(searches)-1])
	}
}

// Both attempts miss — error should mention both variants.
func TestWikidataProvider_GetLogo_NoMatchAfterStrip(t *testing.T) {
	srv := stubWikidata(t,
		`{"search":[]}`,
		``, ``, http.StatusOK,
	)
	defer srv.Close()

	p := newWikidataProviderWithServer(srv)
	_, err := p.GetLogo(context.Background(), "XYZ", "Made Up Co., Inc.")
	if err == nil {
		t.Fatal("expected error when both searches miss")
	}
	if !strings.Contains(err.Error(), "Made Up Co., Inc.") {
		t.Errorf("error should mention the normalized name: %v", err)
	}
}
