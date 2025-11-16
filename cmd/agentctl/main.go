package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// ioStreams wires stdout/stderr for commands and becomes injectable in tests.
type ioStreams struct {
	out io.Writer
	err io.Writer
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	streams := ioStreams{out: os.Stdout, err: os.Stderr}
	if err := runCLI(ctx, os.Args[1:], streams); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintln(streams.err, err)
		}
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, argv []string, streams ioStreams) error {
	global := flag.NewFlagSet("agentctl", flag.ContinueOnError)
	global.SetOutput(streams.err)
	configPath := defaultConfigPath()
	global.StringVar(&configPath, "config", configPath, "Path to CLI config file (defaults to ~/.agentsdk/config.json).")
	global.Usage = func() {
		fmt.Fprintln(streams.err, "agentctl - agentsdk-go control surface")
		fmt.Fprintln(streams.err, "\nUsage:")
		fmt.Fprintln(streams.err, "  agentctl [global flags] <command> [args]")
		fmt.Fprintln(streams.err, "\nCommands:")
		fmt.Fprintln(streams.err, "  run     Execute a single task once")
		fmt.Fprintln(streams.err, "  serve   Start the HTTP API server")
		fmt.Fprintln(streams.err, "  config  Manage local configuration")
		fmt.Fprintln(streams.err, "\nGlobal Flags:")
		global.PrintDefaults()
		fmt.Fprintln(streams.err, "\nRun 'agentctl <command> -h' for command-specific usage.")
	}
	if err := global.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args := global.Args()
	if len(args) == 0 {
		global.Usage()
		return fmt.Errorf("missing command")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "run":
		return runCommand(ctx, rest, configPath, streams)
	case "serve":
		return serveCommand(ctx, rest, configPath, streams)
	case "config":
		return configCommand(rest, configPath, streams)
	case "help", "-h", "--help":
		global.Usage()
		return nil
	default:
		global.Usage()
		return fmt.Errorf("unknown command %q", sub)
	}
}
