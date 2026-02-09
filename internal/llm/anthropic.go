package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// AnthropicClient implements the Client interface using Claude with native web search.
// Claude's built-in web_search tool lets it search the web autonomously and return
// structured results — no need to orchestrate a separate search API.
type AnthropicClient struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicClient creates a new Claude-powered logo finder.
func NewAnthropicClient(apiKey string, model string) *AnthropicClient {
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
	)
	return &AnthropicClient{
		client: &client,
		model:  model,
	}
}

func (a *AnthropicClient) ProviderName() string { return "anthropic" }
func (a *AnthropicClient) ModelName() string     { return a.model }

// submitLogoResult is the schema for the custom tool Claude calls to return results.
// We define a tool so Claude returns structured data instead of free-form text.
type submitLogoResult struct {
	LogoURL     string `json:"logo_url"`
	CompanyName string `json:"company_name"`
	Source      string `json:"source"`
	Confidence  string `json:"confidence"`
}

func (a *AnthropicClient) FindLogoURL(ctx context.Context, symbol string, companyName string) (*LogoSearchResult, error) {
	prompt := buildPrompt(symbol, companyName)

	// Define our custom tool for structured output.
	// Claude will call this tool to "submit" its answer, giving us clean JSON
	// instead of parsing free-form text.
	submitTool := anthropic.ToolParam{
		Name:        "submit_logo_url",
		Description: param.NewOpt("Submit the logo URL you found. Call this tool once you have found the best logo URL."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]interface{}{
				"logo_url": map[string]interface{}{
					"type":        "string",
					"description": "Direct URL to the logo image (PNG, SVG, or JPG). Must be a direct image URL, not a webpage.",
				},
				"company_name": map[string]interface{}{
					"type":        "string",
					"description": "The official company name for this stock ticker.",
				},
				"source": map[string]interface{}{
					"type":        "string",
					"description": "The website where the logo was found (e.g., 'wikipedia.org', 'company.com').",
				},
				"confidence": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"high", "medium", "low"},
					"description": "How confident you are this is the correct official logo.",
				},
			},
		},
	}

	// Two tools: web_search (built-in) + submit_logo_url (custom).
	// Claude searches the web, finds the logo, then calls submit_logo_url.
	tools := []anthropic.ToolUnionParam{
		// Web search is a built-in tool — the SDK has a dedicated struct for it.
		// The Name and Type fields use Go constant types that auto-fill their values.
		{OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{}},
		{OfTool: &submitTool},
	}

	// Agentic loop: Claude may need multiple turns (search → read results → search more → submit).
	// We keep sending tool results back until Claude calls submit_logo_url or gives up.
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
	}

	for i := 0; i < 5; i++ { // Max 5 turns to prevent runaway
		message, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(a.model),
			MaxTokens: 1024,
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			return nil, fmt.Errorf("anthropic API call: %w", err)
		}

		// Check if Claude called our submit tool
		for _, block := range message.Content {
			toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}

			if toolUse.Name == "submit_logo_url" {
				// Parse the structured result
				inputBytes, err := json.Marshal(toolUse.Input)
				if err != nil {
					return nil, fmt.Errorf("marshaling tool input: %w", err)
				}

				var result submitLogoResult
				if err := json.Unmarshal(inputBytes, &result); err != nil {
					return nil, fmt.Errorf("parsing tool input: %w", err)
				}

				if result.LogoURL == "" {
					return nil, fmt.Errorf("Claude did not find a logo URL for %s", symbol)
				}

				return &LogoSearchResult{
					LogoURL:     result.LogoURL,
					CompanyName: result.CompanyName,
					Source:      result.Source,
					Confidence:  result.Confidence,
				}, nil
			}
		}

		// Claude hasn't submitted yet — it might be doing web searches.
		if message.StopReason == "end_turn" {
			return nil, fmt.Errorf("Claude ended without finding a logo for %s", symbol)
		}

		// Add Claude's response to conversation for the next turn.
		// Web search tool results are handled automatically by the API.
		messages = append(messages, message.ToParam())

		// Provide tool results for custom tool calls (not web_search — that's automatic)
		toolResults := []anthropic.ContentBlockParamUnion{}
		for _, block := range message.Content {
			toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok || toolUse.Name == "web_search" {
				continue
			}
			if toolUse.Name != "submit_logo_url" {
				toolResults = append(toolResults,
					anthropic.NewToolResultBlock(toolUse.ID, "Received, please continue searching.", false))
			}
		}
		if len(toolResults) > 0 {
			messages = append(messages, anthropic.NewUserMessage(toolResults...))
		}
	}

	return nil, fmt.Errorf("exceeded max turns without finding logo for %s", symbol)
}

// buildPrompt creates the user prompt for the LLM.
func buildPrompt(symbol string, companyName string) string {
	hint := ""
	if companyName != "" {
		hint = fmt.Sprintf(" (company name: %s)", companyName)
	}

	return fmt.Sprintf(`Find the official company logo for stock ticker symbol "%s"%s.

Search the web to find a high-quality logo image. Prefer:
1. Official company website logos
2. Wikipedia commons logos (often high-quality SVG/PNG)
3. Well-known financial data sites

Requirements for the logo URL:
- Must be a DIRECT link to an image file (ending in .png, .svg, .jpg, or similar)
- Must be a high-resolution version (at least 200x200 pixels)
- Must be the company's primary/official logo (not a product logo or icon variant)
- The URL must be publicly accessible (no authentication required)

Once you find the best logo, call the submit_logo_url tool with the URL and details.
If you cannot find a suitable logo, explain why in your response.`, symbol, hint)
}
