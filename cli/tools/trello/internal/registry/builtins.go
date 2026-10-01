package registry

import (
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

type noInput struct{}

type describeInput struct {
	Name string `json:"name,omitempty" jsonschema:"Command name; omit for every command" cli:"positional"`
}

type teachInput struct {
	Topic string `json:"topic,omitempty" jsonschema:"Topic name; omit for the orientation" cli:"positional"`
	Item  string `json:"item,omitempty" jsonschema:"One item within the topic" cli:"positional"`
}

type datasetIDInput struct {
	ID string `json:"id" jsonschema:"Dataset id from meta.dataset.id" cli:"positional"`
}

type datasetReadInput struct {
	ID string `json:"id,omitempty" jsonschema:"Dataset id from meta.dataset.id; optional with --cursor" cli:"positional"`
}

// DatasetSummary is one entry of dataset list.
type DatasetSummary struct {
	ID          string `json:"id"`
	Command     string `json:"command"`
	RecordCount int64  `json:"record_count"`
	Bytes       int64  `json:"bytes"`
	Complete    bool   `json:"complete"`
	AgeSeconds  int64  `json:"age_seconds"`
}

var localErrors = []errs.Code{errs.Usage}

// Builtins returns the contract-provided commands (§6, §7, §10.3, §16, §18).
func Builtins() []*Command {
	datasetErrors := []errs.Code{errs.Usage, errs.Config, errs.CacheMiss}
	return []*Command{
		Builtin[noInput](Spec{Name: "tools", Description: "List every command name.", Errors: localErrors}, false),
		Builtin[describeInput](Spec{Name: "describe", Description: "Full schema for one command, or for all of them.", Errors: localErrors}, false),
		Builtin[teachInput](Spec{Name: "teach", Description: "Markdown guidance: orientation, contract, flags, config, and domain topics.", Errors: localErrors}, false),
		Builtin[noInput](Spec{Name: "doctor", Description: "Check configuration, credentials, network, authentication, clock, and datasets.",
			Errors: []errs.Code{errs.Config, errs.Auth, errs.Network, errs.Timeout, errs.Upstream}}, false),
		Builtin[noInput](Spec{Name: "list-config", Description: "Every setting, its value, and where it came from.", Errors: []errs.Code{errs.Usage, errs.Config}}, false),
		Builtin[noInput](Spec{Name: "version", Description: "Tool and contract versions and build flavour.", Errors: localErrors}, false),
		Builtin[noInput](Spec{Name: "serve", Description: "Run as an MCP server over stdio or Streamable HTTP.", Errors: []errs.Code{errs.Usage, errs.Config, errs.Refused}}, false),
		Builtin[datasetReadInput](Spec{Name: "dataset.read", Description: "Read part of a stored dataset. Never contacts upstream.",
			Limits: &Limits{Default: 25, Max: 1000}, Errors: datasetErrors}, true),
		Builtin[noInput](Spec{Name: "dataset.list", Description: "List stored datasets.",
			Limits: &Limits{Default: 25, Max: 1000}, Sort: "age_seconds asc, id asc",
			Fields: &FieldSet{
				Default:   []string{"id", "command", "record_count", "bytes", "complete", "age_seconds"},
				Available: []string{"id", "command", "record_count", "bytes", "complete", "age_seconds"},
			}, Errors: datasetErrors}, true),
		Builtin[datasetIDInput](Spec{Name: "dataset.stat", Description: "Show a dataset's metadata.", Errors: datasetErrors}, false),
		Builtin[datasetIDInput](Spec{Name: "dataset.rm", Description: "Delete one dataset.", Errors: datasetErrors}, false),
		Builtin[noInput](Spec{Name: "dataset.clear", Description: "Delete every dataset. Requires --confirm.", Errors: append(datasetErrors, errs.Refused)}, false),
	}
}
