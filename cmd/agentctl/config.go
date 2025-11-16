package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	configDirName  = ".agentsdk"
	configFileName = "config.json"
)

type cliConfig struct {
	DefaultModel string   `json:"default_model"`
	APIKey       string   `json:"api_key"`
	BaseURL      string   `json:"base_url"`
	MCPServers   []string `json:"mcp_servers"`
}

func (c *cliConfig) normalize() {
	if c == nil {
		return
	}
	c.DefaultModel = strings.TrimSpace(c.DefaultModel)
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.MCPServers = uniqueStrings(c.MCPServers)
}

func configCommand(argv []string, cfgPath string, streams ioStreams) error {
	set := flag.NewFlagSet("config", flag.ContinueOnError)
	set.SetOutput(streams.err)
	configFlag := set.String("config", cfgPath, "Path to CLI config file.")
	set.Usage = func() {
		fmt.Fprintln(streams.err, "Usage: agentctl config [flags] <init|set|get|list> ...")
		fmt.Fprintln(streams.err, "\nCommands:")
		fmt.Fprintln(streams.err, "  init             Create a new config file with defaults")
		fmt.Fprintln(streams.err, "  set key value    Update a single key")
		fmt.Fprintln(streams.err, "  get key          Print the value of a key")
		fmt.Fprintln(streams.err, "  list             Show all configuration values")
		fmt.Fprintln(streams.err, "\nFlags:")
		set.PrintDefaults()
	}
	if err := set.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cfgPath = *configFlag
	args := set.Args()
	if len(args) == 0 {
		set.Usage()
		return errors.New("config expects a subcommand")
	}
	sub := args[0]
	switch sub {
	case "init":
		return configInit(cfgPath, streams.out)
	case "set":
		return configSet(cfgPath, args[1:], streams.out)
	case "get":
		return configGet(cfgPath, args[1:], streams.out)
	case "list":
		return configList(cfgPath, streams.out)
	default:
		return fmt.Errorf("unknown config subcommand %q", sub)
	}
}

func configInit(path string, out io.Writer) error {
	resolved, err := expandConfigPath(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(resolved); err == nil {
		return fmt.Errorf("config already exists at %s", resolved)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check config: %w", err)
	}
	if err := ensureConfigDir(resolved); err != nil {
		return err
	}
	if err := saveCLIConfig(resolved, cliConfig{}); err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintf(out, "created %s\n", resolved)
	}
	return nil
}

func configSet(path string, args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New("config set requires <key> <value>")
	}
	key := strings.ToLower(strings.TrimSpace(args[0]))
	value := strings.TrimSpace(strings.Join(args[1:], " "))
	resolved, err := expandConfigPath(path)
	if err != nil {
		return err
	}
	cfg, err := loadCLIConfig(resolved)
	if err != nil {
		return err
	}
	switch key {
	case "default_model":
		cfg.DefaultModel = value
	case "api_key":
		cfg.APIKey = value
	case "base_url":
		cfg.BaseURL = value
	case "mcp_servers":
		cfg.MCPServers = parseList(value)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	if err := saveCLIConfig(resolved, cfg); err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintf(out, "%s updated\n", key)
	}
	return nil
}

func configGet(path string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("config get requires a key")
	}
	key := strings.ToLower(strings.TrimSpace(args[0]))
	resolved, err := expandConfigPath(path)
	if err != nil {
		return err
	}
	cfg, err := loadCLIConfig(resolved)
	if err != nil {
		return err
	}
	value, err := configValue(cfg, key)
	if err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintln(out, value)
	}
	return nil
}

func configList(path string, out io.Writer) error {
	resolved, err := expandConfigPath(path)
	if err != nil {
		return err
	}
	cfg, err := loadCLIConfig(resolved)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	fmt.Fprintf(out, "default_model=%s\n", cfg.DefaultModel)
	fmt.Fprintf(out, "api_key=%s\n", cfg.APIKey)
	fmt.Fprintf(out, "base_url=%s\n", cfg.BaseURL)
	fmt.Fprintf(out, "mcp_servers=%s\n", strings.Join(cfg.MCPServers, ","))
	return nil
}

func configValue(cfg cliConfig, key string) (string, error) {
	switch key {
	case "default_model":
		return cfg.DefaultModel, nil
	case "api_key":
		return cfg.APIKey, nil
	case "base_url":
		return cfg.BaseURL, nil
	case "mcp_servers":
		return strings.Join(cfg.MCPServers, ","), nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func loadCLIConfig(path string) (cliConfig, error) {
	resolved, err := expandConfigPath(path)
	if err != nil {
		return cliConfig{}, err
	}
	data, err := os.ReadFile(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return cliConfig{}, nil
	}
	if err != nil {
		return cliConfig{}, fmt.Errorf("read config: %w", err)
	}
	var cfg cliConfig
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cliConfig{}, fmt.Errorf("parse config: %w", err)
		}
	}
	cfg.normalize()
	return cfg, nil
}

func saveCLIConfig(path string, cfg cliConfig) error {
	resolved, err := expandConfigPath(path)
	if err != nil {
		return err
	}
	cfg.normalize()
	if err := ensureConfigDir(resolved); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return os.WriteFile(resolved, data, 0o600)
}

func ensureConfigDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", configFileName)
	}
	return filepath.Join(home, configDirName, configFileName)
}

func expandConfigPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		trimmed = defaultConfigPath()
	}
	if trimmed == "~" {
		trimmed = filepath.Join("~", configDirName, configFileName)
	}
	if strings.HasPrefix(trimmed, "~/") || trimmed == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))
	}
	clean := filepath.Clean(trimmed)
	if filepath.IsAbs(clean) {
		return clean, nil
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func parseList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	return uniqueStrings(fields)
}
