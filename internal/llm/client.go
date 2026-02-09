// Package llm provides a provider-agnostic interface for using LLMs to find
// stock ticker logos via web search. The LLM searches the web for the company's
// official logo and returns a direct image URL.
package llm

import "context"

// LogoSearchResult contains the result of an LLM-powered logo search.
type LogoSearchResult struct {
	LogoURL     string // Direct URL to the logo image
	CompanyName string // Confirmed company name
	Source      string // Where the logo was found (e.g., "wikipedia.org")
	Confidence  string // "high", "medium", "low"
}

// Client is the interface for LLM providers that can search for logos.
// Both Anthropic (Claude) and OpenAI implement this interface, allowing
// the service to fall back from one to the other.
//
// Go interface design tip: keep interfaces small. This has one method —
// that's ideal. The bigger the interface, the harder it is to implement
// and mock. Go proverb: "The bigger the interface, the weaker the abstraction."
type Client interface {
	FindLogoURL(ctx context.Context, symbol string, companyName string) (*LogoSearchResult, error)
	ProviderName() string
	ModelName() string
}
