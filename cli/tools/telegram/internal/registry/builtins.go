package registry

import (
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
)

type noInput struct{}

type describeInput struct {
	Name string `json:"name,omitempty" jsonschema:"Command name; omit for every command" cli:"positional"`
}

type teachInput struct {
	Topic string `json:"topic,omitempty" jsonschema:"Topic name; omit for the orientation" cli:"positional"`
	Item  string `json:"item,omitempty" jsonschema:"One item within the topic" cli:"positional"`
}

var localErrors = []errs.Code{errs.Usage}

// Builtins returns the contract-provided commands (§6, §7, §10.3, §16, §18).
func Builtins() []*Command {
	return []*Command{
		Builtin[noInput](Spec{Name: "tools", Description: "List every command name.", Errors: localErrors}, false),
		Builtin[describeInput](Spec{Name: "describe", Description: "Full schema for one command, or for all of them.", Errors: localErrors}, false),
		Builtin[teachInput](Spec{Name: "teach", Description: "Markdown guidance: orientation, contract, flags, config, and domain topics.", Errors: localErrors}, false),
		Builtin[noInput](Spec{Name: "doctor", Description: "Check configuration, credentials, network, bot authentication, clock, and datasets.",
			Errors: []errs.Code{errs.Config, errs.Auth, errs.Network, errs.Timeout, errs.Upstream}}, false),
		Builtin[noInput](Spec{Name: "list-config", Description: "Every setting, its value, and where it came from.", Errors: []errs.Code{errs.Usage, errs.Config}}, false),
		Builtin[noInput](Spec{Name: "list-profiles", Description: "Profiles this tool can run with: default first, or [] when none is declared.", Errors: []errs.Code{errs.Usage, errs.Config}}, false),
		Builtin[noInput](Spec{Name: "version", Description: "Tool and contract versions and build flavour.", Errors: localErrors}, false),
		Builtin[noInput](Spec{Name: "serve", Description: "Run as an MCP server over stdio or Streamable HTTP.", Errors: []errs.Code{errs.Usage, errs.Config, errs.Refused}}, false),
	}
}
