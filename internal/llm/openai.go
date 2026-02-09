package llm

import (
	"context"
	"encoding/json"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIClient implements the Client interface using OpenAI's API as a fallback.
// Uses function calling to get structured logo URL results.
type OpenAIClient struct {
	client *openai.Client
	model  string
}

// NewOpenAIClient creates a new OpenAI-powered logo finder.
func NewOpenAIClient(apiKey string, model string) *OpenAIClient {
	return &OpenAIClient{
		client: openai.NewClient(apiKey),
		model:  model,
	}
}

func (o *OpenAIClient) ProviderName() string { return "openai" }
func (o *OpenAIClient) ModelName() string     { return o.model }

func (o *OpenAIClient) FindLogoURL(ctx context.Context, symbol string, companyName string) (*LogoSearchResult, error) {
	prompt := buildPrompt(symbol, companyName)

	// Define the submit_logo_url function for structured output.
	// OpenAI's Parameters field accepts `any` — we pass a raw JSON schema map.
	tools := []openai.Tool{
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "submit_logo_url",
				Description: "Submit the logo URL found for the stock ticker. Call this once you have found the best logo URL.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"logo_url": map[string]interface{}{
							"type":        "string",
							"description": "Direct URL to the logo image (PNG, SVG, or JPG).",
						},
						"company_name": map[string]interface{}{
							"type":        "string",
							"description": "The official company name.",
						},
						"source": map[string]interface{}{
							"type":        "string",
							"description": "Website where the logo was found.",
						},
						"confidence": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"high", "medium", "low"},
							"description": "Confidence level.",
						},
					},
					"required": []string{"logo_url", "company_name", "confidence"},
				},
			},
		},
	}

	messages := []openai.ChatCompletionMessage{
		{
			Role: openai.ChatMessageRoleSystem,
			Content: `You are a logo finder assistant. Search the web to find official company logos for stock tickers.
Return the direct image URL via the submit_logo_url function. Prefer high-resolution PNG/SVG from official sources.`,
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: prompt,
		},
	}

	// OpenAI tool calling loop
	for i := 0; i < 5; i++ {
		resp, err := o.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    o.model,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return nil, fmt.Errorf("openai API call: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("openai returned no choices")
		}

		choice := resp.Choices[0]

		// Check for tool calls
		if len(choice.Message.ToolCalls) > 0 {
			messages = append(messages, choice.Message)

			for _, toolCall := range choice.Message.ToolCalls {
				if toolCall.Function.Name == "submit_logo_url" {
					var result submitLogoResult
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &result); err != nil {
						return nil, fmt.Errorf("parsing tool arguments: %w", err)
					}

					if result.LogoURL == "" {
						return nil, fmt.Errorf("OpenAI did not find a logo URL for %s", symbol)
					}

					return &LogoSearchResult{
						LogoURL:     result.LogoURL,
						CompanyName: result.CompanyName,
						Source:      result.Source,
						Confidence:  result.Confidence,
					}, nil
				}

				// For other tool calls, send a generic result back
				messages = append(messages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    "Received. Please continue and call submit_logo_url with the logo URL.",
					ToolCallID: toolCall.ID,
				})
			}
			continue
		}

		// No tool calls — model finished without calling our tool
		if choice.FinishReason == "stop" {
			return nil, fmt.Errorf("OpenAI ended without finding a logo for %s", symbol)
		}
	}

	return nil, fmt.Errorf("exceeded max turns without finding logo for %s", symbol)
}
