package app

import (
	"sort"
	"strconv"
	"strings"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
)

// Invocation is one parsed call: from argv, or from an MCP tool call.
type Invocation struct {
	Command *registry.Command
	Params  map[string]any
	Flags   map[string]string // reserved flags given, by name without dashes; booleans are "true"
}

func (i *Invocation) Bool(name string) bool {
	value, ok := i.Flags[name]
	return ok && value != "false"
}

func (i *Invocation) Has(name string) bool {
	_, ok := i.Flags[name]
	return ok
}

// ScanSecretFlags refuses a credential passed as an argument before anything parses argv, so the
// operator learns the credential leaked even when the rest of the line is wrong (contract §12.2).
func ScanSecretFlags(args []string, prefix string) *errs.Error {
	for _, arg := range args {
		if arg == "--" {
			return nil
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		for _, forbidden := range registry.ForbiddenFlags {
			if name == forbidden {
				return errs.New(errs.Refused,
					"A credential was passed on the command line and is now in your shell history and logs; rotate it.").
					WithHint("Revoke the token with @BotFather (/revoke), then set "+prefix+"_BOT_TOKEN_FILE=<path> instead.").
					WithDetail("reason", "secret_on_argv").WithDetail("flag", "--"+name)
			}
		}
	}
	return nil
}

type token struct {
	text  string
	flag  string // name without dashes, for flags
	value string
	has   bool // value given with =
}

// Parse turns argv into an invocation. It never prints help or reads anything but its arguments.
// disabled names the mutating commands a read-only build refuses; it is empty when writes are built.
func Parse(args []string, reg *registry.Registry, disabled []string) (*Invocation, *errs.Error) {
	positionals, flags := tokenize(args, reg)
	command, rest, err := resolveCommand(positionals, reg, disabled, flags)
	return bind(command, rest, flags, err)
}

// tokenize splits argv into positionals and flags, giving value-taking flags their next argument.
func tokenize(args []string, reg *registry.Registry) ([]string, []token) {
	valueFlags := valueTakingFlags(reg)
	var positionals []string
	var flags []token
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "-h" {
			flags = append(flags, token{text: arg, flag: "help"})
			continue
		}
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			positionals = append(positionals, arg)
			continue
		}
		name, value, has := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		current := token{text: arg, flag: name, value: value, has: has}
		if !has && valueFlags[name] && index+1 < len(args) {
			current.value, current.has = args[index+1], true
			index++
		}
		flags = append(flags, current)
	}
	return positionals, flags
}

// Recognise returns the command argv names, or nil when it names none. A mutating command that a
// read-only build refuses is still recognised (contract §3.1, §11.3).
func Recognise(args []string, reg *registry.Registry, disabled []string) *string {
	positionals, flags := tokenize(args, reg)
	command, _, err := resolveCommand(positionals, reg, disabled, flags)
	if command != nil {
		return &command.Name
	}
	if err != nil && err.Details["reason"] == "writes_disabled" {
		if name, ok := err.Details["command"].(string); ok {
			return &name
		}
	}
	return nil
}

// bind turns a resolved command and its arguments into an invocation.
func bind(command *registry.Command, rest []string, flags []token, err *errs.Error) (*Invocation, *errs.Error) {
	if err != nil {
		return nil, err
	}
	if command == nil {
		return nil, nil // bare --help
	}
	invocation := &Invocation{Command: command, Params: map[string]any{}, Flags: map[string]string{}}

	for _, flag := range flags {
		if reserved := registry.ReservedFlag(flag.flag); reserved != nil {
			if !registry.FlagApplies(reserved, command) {
				return nil, errs.Usagef("--%s does not apply to %s.", flag.flag, strings.Join(command.Argv(), " ")).
					WithHint(registry.ToolName+" describe "+command.Name).
					WithDetail("unknown_flag", "--"+flag.flag)
			}
			value := flag.value
			if reserved.Value == "" {
				if !flag.has {
					value = "true"
				} else if value != "true" && value != "false" {
					return nil, errs.Usagef("--%s takes no value.", flag.flag).WithDetail("flag", "--"+flag.flag)
				}
			} else if !flag.has {
				return nil, errs.Usagef("--%s needs a value: --%s %s.", flag.flag, flag.flag, reserved.Value).
					WithDetail("flag", "--"+flag.flag)
			}
			invocation.Flags[flag.flag] = value
			continue
		}
		param := paramForFlag(command, flag.flag)
		if param == nil {
			if refusal := command.UnavailableParam(flag.flag); refusal != nil {
				return nil, refusal
			}
			err := errs.Usagef("Unknown flag --%s for %s.", flag.flag, strings.Join(command.Argv(), " ")).
				WithHint(registry.ToolName+" describe "+command.Name).
				WithDetail("unknown_flag", "--"+flag.flag)
			if suggestion := shape.Closest(flag.flag, applicableFlagNames(command)); suggestion != "" {
				err.WithDetail("did_you_mean", "--"+suggestion)
			}
			return nil, err
		}
		value, convErr := convert(param, flag)
		if convErr != nil {
			return nil, convErr
		}
		invocation.Params[param.Name] = value
	}

	// Positionals are checked after flags: a mistyped flag strands its value as a positional, and
	// the flag is the error worth naming (contract §3.3).
	positionalNames := command.Positional()
	if len(rest) > len(positionalNames) {
		return nil, errs.Usagef("Unexpected argument %q for %s.", rest[len(positionalNames)], strings.Join(command.Argv(), " ")).
			WithHint(registry.ToolName+" describe "+command.Name).
			WithDetail("unexpected", rest[len(positionalNames)])
	}
	for index, value := range rest {
		invocation.Params[positionalNames[index]] = value
	}
	return invocation, nil
}

func resolveCommand(positionals []string, reg *registry.Registry, disabled []string, flags []token) (*registry.Command, []string, *errs.Error) {
	for length := len(positionals); length >= 1; length-- {
		if command := reg.Get(strings.Join(positionals[:length], ".")); command != nil {
			return command, positionals[length:], nil
		}
	}
	if len(positionals) == 0 {
		for _, flag := range flags {
			switch flag.flag {
			case "version":
				return reg.Get("version"), nil, nil
			case "help":
				return nil, nil, nil
			}
		}
		return nil, nil, errs.Usagef("No command given.").WithHint(registry.ToolName + " teach")
	}
	for length := len(positionals); length >= 1; length-- {
		name := strings.Join(positionals[:length], ".")
		for _, mutating := range disabled {
			if name == mutating {
				return nil, nil, errs.New(errs.Refused, "This build is read-only; %s changes Telegram and is not available.",
					strings.Join(positionals[:length], " ")).
					WithDetail("reason", "writes_disabled").WithDetail("command", name)
			}
		}
	}
	attempted := strings.Join(positionals[:min(len(positionals), 3)], " ")
	err := errs.Usagef("Unknown command: %s.", attempted).
		WithHint(registry.ToolName+" tools").
		WithDetail("unknown_command", attempted)
	var candidates []string
	for _, command := range reg.Commands() {
		candidates = append(candidates, strings.Join(command.Argv(), " "))
	}
	probe := strings.Join(positionals[:min(len(positionals), 2)], " ")
	if suggestion := shape.Closest(probe, candidates); suggestion != "" {
		err.WithDetail("did_you_mean", suggestion)
	}
	return nil, nil, err
}

func valueTakingFlags(reg *registry.Registry) map[string]bool {
	flags := map[string]bool{}
	for _, flag := range registry.ReservedFlags {
		if flag.Value != "" {
			flags[flag.Name] = true
		}
	}
	for _, command := range reg.Commands() {
		for _, param := range command.Params {
			if !param.Positional && param.Type != registry.TypeBoolean {
				flags[strings.TrimPrefix(param.Flag, "--")] = true
			}
		}
	}
	return flags
}

func paramForFlag(command *registry.Command, name string) *registry.Param {
	for index := range command.Params {
		param := &command.Params[index]
		if !param.Positional && param.Flag == "--"+name {
			return param
		}
	}
	return nil
}

func applicableFlagNames(command *registry.Command) []string {
	var names []string
	for index := range registry.ReservedFlags {
		if registry.FlagApplies(&registry.ReservedFlags[index], command) {
			names = append(names, registry.ReservedFlags[index].Name)
		}
	}
	for _, param := range command.Params {
		if !param.Positional {
			names = append(names, strings.TrimPrefix(param.Flag, "--"))
		}
	}
	sort.Strings(names)
	return names
}

func convert(param *registry.Param, flag token) (any, *errs.Error) {
	switch param.Type {
	case registry.TypeBoolean:
		if !flag.has || flag.value == "true" {
			return true, nil
		}
		if flag.value == "false" {
			return false, nil
		}
		return nil, errs.Usagef("%s takes true or false.", param.Flag)
	case registry.TypeInteger:
		number, err := strconv.ParseInt(flag.value, 10, 64)
		if err != nil {
			return nil, errs.Usagef("%s must be a whole number.", param.Flag)
		}
		return number, nil
	default:
		if !flag.has {
			return nil, errs.Usagef("%s needs a value.", param.Flag).WithDetail("flag", param.Flag)
		}
		return flag.value, nil
	}
}
