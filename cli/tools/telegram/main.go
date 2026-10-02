// Command telegram is an agent-facing command line for the Telegram Bot API, conforming to the Agent
// CLI Contract. It keeps the bot token in its own environment and prints one JSON document per call.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/app"
	"github.com/guyravid/ai/cli/tools/telegram/internal/commands"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
)

// Stamped at build time with -ldflags "-X main.version=… -X main.commit=…".
var (
	version = "0.1.0-dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	all := append(registry.Builtins(), commands.Reads()...)
	all = append(all, commands.Writes()...)
	reg := registry.New(all)

	application := &app.App{
		Build: app.Build{
			Tool: "telegram", Prefix: "TELEGRAM", Version: version, Commit: commit,
			WritesEnabled: commands.WritesEnabled, MCPEnabled: serveFunc != nil,
			GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		},
		Registry:      reg,
		Env:           os.LookupEnv,
		Environ:       os.Environ,
		Now:           time.Now,
		Stderr:        os.Stderr,
		Serve:         serveFunc,
		MutatingNames: commands.MutatingNames,
		// A registry defect is caught before any command runs and reported as internal.
		StartupError: errors.Join(reg.Validate(), reg.ValidateMutating(commands.MutatingNames)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var received atomic.Int32
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signals
		if sig == syscall.SIGTERM {
			received.Store(143)
		} else {
			received.Store(130)
		}
		cancel()
	}()
	return application.Run(ctx, os.Args[1:], os.Stdout, func() int { return int(received.Load()) })
}
