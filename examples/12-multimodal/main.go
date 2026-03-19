// Package main demonstrates multimodal content block support.
//
// Requires ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN).
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"strings"
	"time"

	"github.com/stellarlinkco/agentsdk-go/examples/internal/demomodel"
	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	modelpkg "github.com/stellarlinkco/agentsdk-go/pkg/model"
)

var (
	multimodalFatal     = log.Fatal
	multimodalPNGEncode = png.Encode
)

type multimodalRuntime interface {
	Run(context.Context, api.Request) (*api.Response, error)
	Close() error
}

var multimodalNewRuntime = func(ctx context.Context, opts api.Options) (multimodalRuntime, error) {
	return api.New(ctx, opts)
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		multimodalFatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	_ = args
	apiKey := demomodel.AnthropicAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) is required")
	}
	opts := api.Options{
		ModelFactory: &modelpkg.AnthropicProvider{
			APIKey:    apiKey,
			BaseURL:   demomodel.AnthropicBaseURL(),
			ModelName: "claude-sonnet-4-5-20250929",
		},
	}

	rt, err := multimodalNewRuntime(ctx, opts)
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := rt.Run(ctx, api.Request{
		ContentBlocks: []modelpkg.ContentBlock{
			{Type: modelpkg.ContentBlockText, Text: "Say hello in exactly 5 words."},
		},
		SessionID: "multimodal-demo-1",
	})
	if err != nil {
		return fmt.Errorf("demo1: %w", err)
	}
	_ = resp

	pngData, err := generateTestPNG()
	if err != nil {
		return fmt.Errorf("generate png: %w", err)
	}
	b64 := base64.StdEncoding.EncodeToString(pngData)

	resp, err = rt.Run(ctx, api.Request{
		ContentBlocks: []modelpkg.ContentBlock{
			{Type: modelpkg.ContentBlockText, Text: "Describe this image in one sentence."},
			{Type: modelpkg.ContentBlockImage, MediaType: "image/png", Data: b64},
		},
		SessionID: "multimodal-demo-2",
	})
	if err != nil {
		return fmt.Errorf("demo2: %w", err)
	}
	_ = resp

	resp, err = rt.Run(ctx, api.Request{
		Prompt: "You are a helpful image analyst.",
		ContentBlocks: []modelpkg.ContentBlock{
			{Type: modelpkg.ContentBlockText, Text: "What is the dominant color in this image?"},
			{Type: modelpkg.ContentBlockImage, MediaType: "image/png", Data: b64},
		},
		SessionID: "multimodal-demo-3",
	})
	if err != nil {
		return fmt.Errorf("demo3: %w", err)
	}
	_ = resp

	return nil
}

// generateTestPNG creates a small 8x8 PNG with a red/blue checkerboard pattern.
func generateTestPNG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if (x+y)%2 == 0 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := multimodalPNGEncode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
