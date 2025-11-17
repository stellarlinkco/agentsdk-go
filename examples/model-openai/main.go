package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/model/openai"
)

const defaultOpenAIModel = "gpt-4.1-mini"

var weatherTools = []map[string]any{{
	"type": "function",
	"function": map[string]any{
		"name":        "lookup_weather",
		"description": "Returns approximate temperature and precipitation for a city.",
		"parameters": map[string]any{
			"type":     "object",
			"required": []string{"city"},
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "City name, e.g. Tokyo or San Francisco.",
				},
			},
		},
	},
}}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is not set")
	}
	model, err := newOpenAIModel(ctx, apiKey)
	if err != nil {
		log.Fatalf("create openai model: %v", err)
	}
	log.Printf("OpenAI model ready: %T (%s)", model, defaultOpenAIModel)

	callModel := func(title, system, user string) modelpkg.Message {
		log.Printf("%s prompt=%q", title, user)
		msgs := []modelpkg.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		}
		reply, err := model.Generate(ctx, msgs)
		if err != nil {
			log.Fatalf("%s: %v", title, err)
		}
		return reply
	}

	syncReply := callModel("---- Generate (sync) ----",
		"You help migrate agentsdk-go examples between providers.",
		"Explain how this OpenAI sample differs from examples/basic.")
	log.Printf("assistant(%s): %s", syncReply.Role, strings.TrimSpace(syncReply.Content))

	streamPrompt := "Stream three numbered steps for calling OpenAI via agentsdk-go."
	log.Printf("---- GenerateStream ---- prompt=%q", streamPrompt)
	err = model.GenerateStream(ctx, []modelpkg.Message{
		{Role: "system", Content: "Stream partial answers so logs show incremental tokens."},
		{Role: "user", Content: streamPrompt},
	}, func(res modelpkg.StreamResult) error {
		if res.Message.Content != "" {
			fmt.Print(res.Message.Content)
		}
		if res.Final {
			fmt.Println()
			log.Println("stream finished")
		}
		return nil
	})
	if err != nil {
		log.Fatalf("stream: %v", err)
	}

	sdkModel, ok := model.(modelpkg.ModelWithTools)
	if !ok {
		log.Println("model does not implement tool calling")
		return
	}
	callReply, err := sdkModel.GenerateWithTools(ctx, []modelpkg.Message{
		{Role: "system", Content: "Call lookup_weather whenever the user mentions weather."},
		{Role: "user", Content: "Do I need sunglasses in Tokyo today?"},
	}, weatherTools)
	if err != nil {
		log.Fatalf("tool call generate: %v", err)
	}
	if len(callReply.ToolCalls) == 0 {
		log.Printf("assistant replied without tools: %s", strings.TrimSpace(callReply.Content))
		return
	}
	for _, call := range callReply.ToolCalls {
		log.Printf("tool request -> name=%s id=%s args=%v", call.Name, call.ID, call.Arguments)
	}
	log.Println("Respond with role=tool + call ID, then call Generate again to finish.")
}

// newOpenAIModel instantiates the OpenAI SDK-backed model used by this demo.
func newOpenAIModel(ctx context.Context, apiKey string) (modelpkg.Model, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openai api key is required")
	}
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL != "" {
		if err := checkOpenAIBaseURL(ctx, baseURL); err != nil {
			return nil, err
		}
		log.Printf("OpenAI model (SDK): %s via %s", defaultOpenAIModel, baseURL)
		return openai.NewSDKModelWithBaseURL(apiKey, defaultOpenAIModel, baseURL, 1024), nil
	}

	log.Printf("OpenAI model (SDK): %s via api.openai.com", defaultOpenAIModel)
	return openai.NewSDKModel(apiKey, defaultOpenAIModel, 1024), nil
}

// checkOpenAIBaseURL performs a lightweight HEAD request to ensure proxy services are reachable.
func checkOpenAIBaseURL(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("OPENAI_BASE_URL %q is invalid: %w", rawURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("OPENAI_BASE_URL must include scheme and host (e.g. https://proxy.example.com)")
	}

	resp, err := doHealthRequest(ctx, http.MethodHead, parsed.String())
	if err != nil {
		resp, err = doHealthRequest(ctx, http.MethodGet, parsed.String())
	}
	if err != nil {
		return fmt.Errorf("cannot reach OpenAI-compatible service at %s: %w.\n请确认服务已启动，或改用官方 OpenAI API。", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("service at %s returned %s — 检查本地服务健康状态，或改用官方 OpenAI API", rawURL, resp.Status)
	}

	return nil
}

func doHealthRequest(ctx context.Context, method, urlStr string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("create base URL health check request: %w", err)
	}

	return http.DefaultClient.Do(req)
}
