package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/guyravid/ai/cli/tools/telegram/internal/envelope"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/render"
	"github.com/guyravid/ai/cli/tools/telegram/internal/secrets"
)

// Run is the command-line entry point. It writes exactly one document to stdout, in one write, and
// returns the exit status. interrupted reports the exit status of a received signal, or 0.
func (a *App) Run(ctx context.Context, args []string, stdout io.Writer, interrupted func() int) int {
	human, pretty := hasFlag(args, "--human"), hasFlag(args, "--pretty")
	response := a.safeRespond(ctx, args, human, pretty)
	if response.Quiet {
		response.Cleanup()
		if code := interrupted(); code != 0 {
			return code
		}
		return response.Exit
	}
	if code := interrupted(); code != 0 {
		response = a.errorResponse(a.commandName(args), errs.CanceledBy(code), pretty)
	}

	output := response.Document()
	if human && response.Text == "" && response.Envelope != nil {
		output = response.redactor.Apply([]byte(render.Human(response.Envelope)))
	}
	if _, err := stdout.Write(output); err != nil {
		return 1
	}
	response.Cleanup()
	return response.Exit
}

// safeRespond recovers a crash anywhere in the call and reports it as internal (base template §4).
func (a *App) safeRespond(ctx context.Context, args []string, human, pretty bool) (response *Response) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = a.errorResponse(a.commandName(args), errs.New(errs.Internal, "The tool failed unexpectedly: %v.", recovered).
				WithHint("Report this with --verbose output."), pretty)
		}
	}()
	return a.respond(ctx, args, human, pretty)
}

// commandName is the command argv names, or nil when it names none. Every envelope names its command
// when argv names one, even when the call fails before or during parsing (contract §3.1).
func (a *App) commandName(args []string) (name *string) {
	defer func() {
		if recover() != nil {
			name = nil
		}
	}()
	if a.Registry == nil {
		return nil
	}
	var disabled []string
	if !a.Build.WritesEnabled {
		disabled = a.MutatingNames
	}
	return Recognise(args, a.Registry, disabled)
}

func (a *App) respond(ctx context.Context, args []string, human, pretty bool) *Response {
	if a.StartupError != nil {
		return a.errorResponse(a.commandName(args), errs.New(errs.Internal, "The command registry is invalid: %s.", a.StartupError.Error()), pretty)
	}
	if err := ScanSecretFlags(args, a.Build.Prefix); err != nil {
		return a.errorResponse(a.commandName(args), err, pretty)
	}
	var disabled []string
	if !a.Build.WritesEnabled {
		disabled = a.MutatingNames
	}
	invocation, err := Parse(args, a.Registry, disabled)
	if err != nil {
		return a.errorResponse(a.commandName(args), err, pretty)
	}
	if invocation == nil {
		return &Response{Text: a.globalHelp(), redactor: secrets.NewRedactor(nil, nil)}
	}
	if invocation.Bool("help") {
		return &Response{Text: a.commandHelp(invocation.Command), redactor: secrets.NewRedactor(nil, nil)}
	}
	if human && pretty {
		name := invocation.Command.Name
		return a.errorResponse(&name, errs.Usagef("--human and --pretty cannot be combined.").
			WithHint("Use --human for people, --pretty for indented JSON."), false)
	}
	return a.Execute(ctx, invocation)
}

// Fail is the error response for a command, for callers outside the argv path such as serve.
func (a *App) Fail(command string, err *errs.Error) *Response {
	return a.errorResponse(&command, err, false)
}

func (a *App) errorResponse(command *string, err *errs.Error, pretty bool) *Response {
	failed := envelope.New(a.Build.Tool, a.Build.Version, command)
	failed.SetError(err)
	return &Response{Envelope: failed, Encoded: Encode(failed, pretty), Exit: failed.ExitCode(),
		redactor: secrets.NewRedactor(nil, nil)}
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == flag || arg == flag+"=true" {
			return true
		}
	}
	return false
}

// globalHelp is human text; the last line points to describe (contract §6.4).
func (a *App) globalHelp() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s %s: agent-facing command line for the Telegram Bot API.\n\n", a.Build.Tool, a.Build.Version)
	fmt.Fprintf(&builder, "Usage: %s <command> [arguments] [flags]\n\nCommands:\n", a.Build.Tool)
	width := 0
	for _, command := range a.Registry.Commands() {
		width = max(width, len(strings.Join(command.Argv(), " ")))
	}
	for _, command := range a.Registry.Commands() {
		fmt.Fprintf(&builder, "  %-*s  %s\n", width, strings.Join(command.Argv(), " "), command.Description)
	}
	fmt.Fprintf(&builder, "\nStart with `%[1]s teach`. Every command prints one JSON document; add --human for text.\n", a.Build.Tool)
	fmt.Fprintf(&builder, "# machine-readable: %s describe\n", a.Build.Tool)
	return builder.String()
}

func (a *App) commandHelp(command *registry.Command) string {
	var builder strings.Builder
	usage := append([]string{a.Build.Tool}, command.Argv()...)
	for _, param := range command.Params {
		switch {
		case param.Positional && param.Required:
			usage = append(usage, "<"+param.Name+">")
		case param.Positional:
			usage = append(usage, "[<"+param.Name+">]")
		case param.Required:
			usage = append(usage, param.Flag+" <"+param.Name+">")
		}
	}
	fmt.Fprintf(&builder, "Usage: %s [flags]\n\n%s\n", strings.Join(usage, " "), command.Description)
	if len(command.Params) > 0 {
		builder.WriteString("\nParameters:\n")
		for _, param := range command.Params {
			name := param.Flag
			if param.Positional {
				name = "<" + param.Name + ">"
			}
			required := ""
			if param.Required {
				required = " (required)"
			}
			fmt.Fprintf(&builder, "  %-16s %s%s\n", name, param.Description, required)
		}
	}
	builder.WriteString("\nFlags:\n")
	for index := range registry.ReservedFlags {
		flag := &registry.ReservedFlags[index]
		if registry.FlagApplies(flag, command) && flag.Name != "help" {
			usage := "--" + flag.Name
			if flag.Value != "" {
				usage += " " + flag.Value
			}
			fmt.Fprintf(&builder, "  %-26s %s\n", usage, flag.Meaning)
		}
	}
	fmt.Fprintf(&builder, "# machine-readable: %s describe %s\n", a.Build.Tool, command.Name)
	return builder.String()
}
