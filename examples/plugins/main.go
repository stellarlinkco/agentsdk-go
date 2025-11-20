package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/plugins"
)

const (
	sampleSignerID  = "example-dev"
	sampleSignerKey = "df307e2950d44ae1f3bfe6a963e67f1364275b32fc2d934914cc29288d81d8f0"
)

type runConfig struct {
	pluginsDir    string
	allowUnsigned bool
}

func main() {
	cfg := parseConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if _, err := os.Stat(cfg.pluginsDir); err != nil {
		log.Fatalf("plugins dir %s: %v", cfg.pluginsDir, err)
	}

	trust, err := buildTrustStore(cfg.allowUnsigned)
	if err != nil {
		log.Fatalf("init trust store: %v", err)
	}

	sampleDir := filepath.Join(cfg.pluginsDir, "sample-plugin")
	manifestPath, err := plugins.FindManifest(sampleDir)
	if err != nil {
		log.Fatalf("locate manifest: %v", err)
	}

	manifest, err := plugins.LoadManifest(manifestPath, plugins.WithRoot(sampleDir), plugins.WithTrustStore(trust))
	if err != nil {
		log.Fatalf("load manifest: %v", err)
	}
	printManifest("Single manifest load", manifest)

	manifests, err := plugins.DiscoverManifests(cfg.pluginsDir, trust)
	if err != nil {
		log.Fatalf("discover manifests: %v", err)
	}

	logger.Info("discovered manifests", "count", len(manifests), "root", cfg.pluginsDir)
	for _, mf := range manifests {
		printManifest(fmt.Sprintf("Plugin %s@%s", mf.Name, mf.Version), mf)
	}
}

func parseConfig() runConfig {
	var cfg runConfig
	flag.StringVar(&cfg.pluginsDir, "plugins", filepath.Join("examples", "plugins"), "plugins root (each subdirectory holds a manifest)")
	flag.BoolVar(&cfg.allowUnsigned, "allow-unsigned", false, "allow unsigned manifests")
	flag.Parse()
	return cfg
}

func buildTrustStore(allowUnsigned bool) (*plugins.TrustStore, error) {
	pub, err := hex.DecodeString(sampleSignerKey)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}

	store := plugins.NewTrustStore()
	store.AllowUnsigned(allowUnsigned)
	store.Register(sampleSignerID, ed25519.PublicKey(pub))
	return store, nil
}

func printManifest(title string, mf *plugins.Manifest) {
	fmt.Printf("\n%s\n%s\n", title, strings.Repeat("-", len(title)))
	fmt.Printf("Name:        %s\n", mf.Name)
	fmt.Printf("Version:     %s\n", mf.Version)
	fmt.Printf("Plugin Dir:  %s\n", mf.PluginDir)
	fmt.Printf("Manifest:    %s\n", mf.ManifestPath)
	fmt.Printf("Digest:      %s\n", mf.Digest)
	fmt.Printf("Signer:      %s\n", mf.Signer)
	fmt.Printf("Trusted:     %t\n", mf.Trusted)
	fmt.Printf("Commands:    %s\n", formatList(mf.Commands))
	fmt.Printf("Agents:      %s\n", formatList(mf.Agents))
	fmt.Printf("Skills:      %s\n", formatList(mf.Skills))
}

func formatList(values []string) string {
	if len(values) == 0 {
		return " <none>"
	}
	return " " + strings.Join(values, ", ")
}
