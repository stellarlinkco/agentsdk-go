// Package main demonstrates multimodal content block support.
//
// Three demos:
//  1. Text-only via ContentBlocks (backward-compatible path)
//  2. Text + base64 image (programmatically generated PNG)
//  3. Text + URL image (public image)
//
// Usage:
//
//	ANTHROPIC_API_KEY=sk-ant-xxx go run ./examples/12-multimodal
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"strings"
	"time"

	"bytes"

	"github.com/cexll/agentsdk-go/pkg/api"
	modelpkg "github.com/cexll/agentsdk-go/pkg/model"
)

func main() {
	provider := &modelpkg.AnthropicProvider{
		ModelName: "claude-sonnet-4-5-20250929",
	}

	rt, err := api.New(context.Background(), api.Options{
		ModelFactory: provider,
	})
	if err != nil {
		log.Fatalf("build runtime: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// ── Demo 1: Text-only via ContentBlocks ──────────────────────────
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println(" Demo 1: Text-only ContentBlocks")
	fmt.Println("═══════════════════════════════════════════════════")

	resp, err := rt.Run(ctx, api.Request{
		ContentBlocks: []modelpkg.ContentBlock{
			{Type: modelpkg.ContentBlockText, Text: "Say hello in exactly 5 words."},
		},
		SessionID: "multimodal-demo-1",
	})
	if err != nil {
		log.Fatalf("demo1: %v", err)
	}
	printResult("Text-only", resp)

	// ── Demo 2: Text + base64 image ──────────────────────────────────
	fmt.Println("\n═══════════════════════════════════════════════════")
	fmt.Println(" Demo 2: Text + Base64 Image")
	fmt.Println("═══════════════════════════════════════════════════")

	pngData := generateTestPNG()
	b64 := base64.StdEncoding.EncodeToString(pngData)

	fmt.Printf("Generated test PNG: %d bytes, base64 length: %d\n\n", len(pngData), len(b64))

	resp, err = rt.Run(ctx, api.Request{
		ContentBlocks: []modelpkg.ContentBlock{
			{Type: modelpkg.ContentBlockText, Text: "Describe this image in one sentence. What colors do you see?"},
			{Type: modelpkg.ContentBlockImage, MediaType: "image/png", Data: b64},
		},
		SessionID: "multimodal-demo-2",
	})
	if err != nil {
		log.Fatalf("demo2: %v", err)
	}
	printResult("Base64 Image", resp)

	// ── Demo 3: Prompt + ContentBlocks combined ──────────────────────
	fmt.Println("\n═══════════════════════════════════════════════════")
	fmt.Println(" Demo 3: Prompt + ContentBlocks (mixed)")
	fmt.Println("═══════════════════════════════════════════════════")

	resp, err = rt.Run(ctx, api.Request{
		Prompt: "You are a helpful image analyst.",
		ContentBlocks: []modelpkg.ContentBlock{
			{Type: modelpkg.ContentBlockText, Text: "What is the dominant color in this image?"},
			{Type: modelpkg.ContentBlockImage, MediaType: "image/png", Data: b64},
		},
		SessionID: "multimodal-demo-3",
	})
	if err != nil {
		log.Fatalf("demo3: %v", err)
	}
	printResult("Prompt+Blocks", resp)

	fmt.Println("\nAll demos completed.")
}

// generateTestPNG creates a small 8x8 PNG with a red/blue checkerboard pattern.
func generateTestPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if (x+y)%2 == 0 {
				img.Set(x, y, color.RGBA{R: 255, A: 255}) // red
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255}) // blue
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatalf("encode PNG: %v", err)
	}
	return buf.Bytes()
}

func printResult(label string, resp *api.Response) {
	if resp.Result == nil {
		fmt.Printf("[%s] No result\n", label)
		return
	}
	output := resp.Result.Output
	lines := strings.Split(strings.TrimSpace(output), "\n")
	fmt.Printf("[%s] Response (%d lines):\n", label, len(lines))
	for _, line := range lines {
		fmt.Printf("  │ %s\n", line)
	}
}
