// Package main demonstrates multi-LLM provider support.
// This example shows how to use both Anthropic and OpenAI providers independently.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cexll/agentsdk-go/pkg/model"
)

func main() {
	providerFlag := flag.String("provider", "anthropic", "LLM provider: anthropic or openai")
	promptFlag := flag.String("prompt", "Say hello and introduce yourself in 2 sentences.", "Prompt to send")
	streamFlag := flag.Bool("stream", false, "Enable streaming mode")
	toolsFlag := flag.Bool("tools", false, "Test tool calling capability")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var mdl model.Model
	var err error

	switch *providerFlag {
	case "anthropic":
		mdl, err = createAnthropicModel()
	case "openai":
		mdl, err = createOpenAIModel()
	default:
		log.Fatalf("Unknown provider: %s. Use 'anthropic' or 'openai'", *providerFlag)
	}

	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	fmt.Printf("Provider: %s\n", *providerFlag)
	fmt.Printf("Streaming: %v\n", *streamFlag)
	fmt.Printf("Tools: %v\n\n", *toolsFlag)

	if *toolsFlag {
		runToolTest(ctx, mdl, *streamFlag)
	} else if *streamFlag {
		fmt.Printf("Prompt: %s\n\n", *promptFlag)
		runStreaming(ctx, mdl, *promptFlag)
	} else {
		fmt.Printf("Prompt: %s\n\n", *promptFlag)
		runComplete(ctx, mdl, *promptFlag)
	}
}

func createAnthropicModel() (model.Model, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_AUTH_TOKEN")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY or ANTHROPIC_AUTH_TOKEN required")
	}

	provider := &model.AnthropicProvider{
		APIKey:    apiKey,
		ModelName: "claude-sonnet-4-5-20250929",
	}

	fmt.Println("Using Anthropic Claude Sonnet 4.5 (claude-sonnet-4-5-20250929)")
	return provider.Model(context.Background())
}

func createOpenAIModel() (model.Model, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY required")
	}

	baseURL := os.Getenv("OPENAI_BASE_URL")
	modelName := "gpt-5.3-codex"

	provider := &model.OpenAIProvider{
		APIKey:    apiKey,
		BaseURL:   baseURL,
		ModelName: modelName,
	}

	if baseURL != "" {
		fmt.Printf("Using OpenAI %s (base: %s)\n", modelName, baseURL)
	} else {
		fmt.Printf("Using OpenAI %s\n", modelName)
	}
	return provider.Model(context.Background())
}

func runComplete(ctx context.Context, mdl model.Model, prompt string) {
	fmt.Println("--- Non-streaming Response ---")

	resp, err := mdl.Complete(ctx, model.Request{
		Messages: []model.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		log.Fatalf("Complete failed: %v", err)
	}

	fmt.Printf("\nResponse:\n%s\n", resp.Message.Content)
	fmt.Printf("\nUsage: input=%d, output=%d, total=%d\n",
		resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens)
	fmt.Printf("Stop reason: %s\n", resp.StopReason)
}

func runStreaming(ctx context.Context, mdl model.Model, prompt string) {
	fmt.Println("--- Streaming Response ---")
	fmt.Print("\n")

	err := mdl.CompleteStream(ctx, model.Request{
		Messages: []model.Message{
			{Role: "user", Content: prompt},
		},
	}, func(result model.StreamResult) error {
		if result.Delta != "" {
			fmt.Print(result.Delta)
		}
		if result.ToolCall != nil {
			fmt.Printf("\n[Tool Call: %s]\n", result.ToolCall.Name)
		}
		if result.Final {
			fmt.Printf("\n\nUsage: input=%d, output=%d, total=%d\n",
				result.Response.Usage.InputTokens,
				result.Response.Usage.OutputTokens,
				result.Response.Usage.TotalTokens)
			fmt.Printf("Stop reason: %s\n", result.Response.StopReason)
		}
		return nil
	})

	if err != nil {
		log.Fatalf("CompleteStream failed: %v", err)
	}
}

// runToolTest tests tool calling capability with a simple calculator tool
func runToolTest(ctx context.Context, mdl model.Model, streaming bool) {
	fmt.Println("--- Tool Calling Test ---")
	fmt.Println("Testing with a calculator tool...")

	// Define a simple calculator tool
	tools := []model.ToolDefinition{
		{
			Name:        "calculator",
			Description: "Perform basic arithmetic operations. Returns the result of the calculation.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"enum":        []string{"add", "subtract", "multiply", "divide"},
						"description": "The arithmetic operation to perform",
					},
					"a": map[string]any{
						"type":        "number",
						"description": "First operand",
					},
					"b": map[string]any{
						"type":        "number",
						"description": "Second operand",
					},
				},
				"required": []string{"operation", "a", "b"},
			},
		},
	}

	prompt := "What is 42 multiplied by 17? Use the calculator tool to compute the exact result."
	fmt.Printf("Prompt: %s\n\n", prompt)

	req := model.Request{
		Messages: []model.Message{
			{Role: "user", Content: prompt},
		},
		Tools: tools,
	}

	if streaming {
		runToolTestStreaming(ctx, mdl, req)
	} else {
		runToolTestComplete(ctx, mdl, req)
	}
}

func runToolTestComplete(ctx context.Context, mdl model.Model, req model.Request) {
	resp, err := mdl.Complete(ctx, req)
	if err != nil {
		log.Fatalf("Complete failed: %v", err)
	}

	fmt.Printf("Response content: %s\n", resp.Message.Content)
	fmt.Printf("Stop reason: %s\n", resp.StopReason)

	if len(resp.Message.ToolCalls) > 0 {
		fmt.Printf("\n=== Tool Calls (%d) ===\n", len(resp.Message.ToolCalls))
		for i, tc := range resp.Message.ToolCalls {
			fmt.Printf("[%d] Tool: %s\n", i+1, tc.Name)
			fmt.Printf("    ID: %s\n", tc.ID)
			fmt.Printf("    Arguments: %v\n", tc.Arguments)
		}
		fmt.Println("\n✓ Tool calling works correctly!")
	} else {
		fmt.Println("\n✗ No tool calls returned (model did not use the tool)")
	}

	fmt.Printf("\nUsage: input=%d, output=%d, total=%d\n",
		resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens)
}

func runToolTestStreaming(ctx context.Context, mdl model.Model, req model.Request) {
	var toolCalls []model.ToolCall

	err := mdl.CompleteStream(ctx, req, func(result model.StreamResult) error {
		if result.Delta != "" {
			fmt.Print(result.Delta)
		}
		if result.ToolCall != nil {
			toolCalls = append(toolCalls, *result.ToolCall)
			fmt.Printf("\n[Tool Call: %s, args=%v]\n", result.ToolCall.Name, result.ToolCall.Arguments)
		}
		if result.Final {
			fmt.Printf("\n\nStop reason: %s\n", result.Response.StopReason)
			fmt.Printf("Usage: input=%d, output=%d, total=%d\n",
				result.Response.Usage.InputTokens,
				result.Response.Usage.OutputTokens,
				result.Response.Usage.TotalTokens)
		}
		return nil
	})

	if err != nil {
		log.Fatalf("CompleteStream failed: %v", err)
	}

	if len(toolCalls) > 0 {
		fmt.Println("\n✓ Tool calling works correctly!")
	} else {
		fmt.Println("\n✗ No tool calls returned (model did not use the tool)")
	}
}
