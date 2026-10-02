package registry

// Scope says which commands a reserved flag applies to.
type Scope string

const (
	ScopeGlobal     Scope = "every command"
	ScopeData       Scope = "commands that return upstream data"
	ScopePaged      Scope = "list commands"
	ScopeUpstream   Scope = "list commands that page through upstream"
	ScopeWindow     Scope = "commands with a time dimension"
	ScopeConfirm    Scope = "write commands"
	ScopeTools      Scope = "tools"
	ScopeTeach      Scope = "teach"
	ScopeListConfig Scope = "list-config"
	ScopeServe      Scope = "serve"
)

// FlagDef is a reserved flag (contract §17). Each has exactly one meaning everywhere.
type FlagDef struct {
	Name     string
	Value    string // placeholder for the value it takes; "" for boolean flags
	Meaning  string
	Section  string
	Scope    Scope
	Setting  string // the setting it sets, if any
	Detail   string // longer explanation for `teach flags <name>`
	MCPExtra bool   // accepted as an MCP tool argument (per-call flags)
}

// ForbiddenFlags may never be defined; seeing one on argv means a credential leaked (§12.2).
// The contract list is extended with bot-token, this tool's own credential name.
var ForbiddenFlags = []string{"api-key", "key", "token", "password", "secret", "credential", "bot-token"}

// ReservedFlags lists every reserved flag in contract order.
var ReservedFlags = []FlagDef{
	{Name: "pretty", Meaning: "Indented JSON", Section: "§14", Scope: ScopeGlobal,
		Detail: "Indents the envelope with two spaces and changes nothing else. Cannot be combined with --human."},
	{Name: "human", Meaning: "Human-readable output", Section: "§15", Scope: ScopeGlobal,
		Detail: "Replaces the envelope with plain text for a person at a terminal. The exit code is unchanged. Leave it off when parsing."},
	{Name: "fields", Value: "<list>", Meaning: "Field projection", Section: "§8.2", Scope: ScopeData, MCPExtra: true,
		Detail: "Chooses the fields in each record: `a,b,c` exactly these in this order; `-a` the defaults minus a; `*` every available field; `badges.*` everything under badges. `describe <name>` lists the available fields."},
	{Name: "keep-empty", Meaning: "Keep empty values", Section: "§8.3", Scope: ScopeGlobal, MCPExtra: true,
		Detail: "By default null, \"\", [] and {} are removed from records. This keeps them."},
	{Name: "limit", Value: "<n>", Meaning: "Records to return", Section: "§8.1", Scope: ScopePaged, Setting: "LIMIT", MCPExtra: true,
		Detail: "How many records to return. The tool pages through upstream itself to fill it. Above the command's maximum it is clamped, with a warning."},
	{Name: "cursor", Value: "<cursor>", Meaning: "Continue from a cursor", Section: "§9.3", Scope: ScopePaged, MCPExtra: true,
		Detail: "Continues a listing. Pass meta.page.next_cursor back alone; it carries the original parameters."},
	{Name: "max-bytes", Value: "<n>", Meaning: "Size cap on the response", Section: "§8.1", Scope: ScopeGlobal, Setting: "MAX_BYTES", MCPExtra: true,
		Detail: "Caps the encoded response. A list drops whole records from the end and sets next_cursor to resume; a single object has its longest strings shortened."},
	{Name: "max-string", Value: "<n>", Meaning: "Characters per string", Section: "§8.1", Scope: ScopeGlobal, Setting: "MAX_STRING", MCPExtra: true,
		Detail: "Longer strings are shortened and their paths listed in meta.elided_fields."},
	{Name: "max-depth", Value: "<n>", Meaning: "Nesting depth of data", Section: "§8.1", Scope: ScopeGlobal, Setting: "MAX_DEPTH", MCPExtra: true,
		Detail: "Anything nested deeper is replaced by \"<depth-elided>\"."},
	{Name: "max-pages", Value: "<n>", Meaning: "Upstream pages per invocation", Section: "§8.1", Scope: ScopeUpstream, Setting: "MAX_PAGES", MCPExtra: true,
		Detail: "Stops paging upstream after this many requests. Stopping early is truncation, not an error."},
	{Name: "since", Value: "<time>", Meaning: "Start of the time window", Section: "§8.5", Scope: ScopeWindow, MCPExtra: true,
		Detail: "Relative (-2h, -30m, -7d), RFC 3339 with an offset, a date (midnight UTC), or epoch seconds or milliseconds. Defaults to -24h."},
	{Name: "until", Value: "<time>", Meaning: "End of the time window", Section: "§8.5", Scope: ScopeWindow, MCPExtra: true,
		Detail: "Same formats as --since, plus now. Defaults to now."},
	{Name: "confirm", Meaning: "Allow a write", Section: "§11.2", Scope: ScopeConfirm, MCPExtra: true,
		Detail: "Without it a write is refused and the refusal shows the exact request as a preview."},
	{Name: "dry-run", Meaning: "Preview without sending", Section: "§11.2", Scope: ScopeGlobal, MCPExtra: true,
		Detail: "Sends no mutating request and returns the would-be request in data.preview. Wins over --confirm."},
	{Name: "profile", Value: "<name>", Meaning: "Select a profile", Section: "§13.3", Scope: ScopeGlobal, Setting: "PROFILE",
		Detail: "Selects a named set of setting overrides."},
	{Name: "config", Value: "<path>", Meaning: "Config file path", Section: "§13.1", Scope: ScopeGlobal, Setting: "CONFIG",
		Detail: "Reads this config file instead of the platform default. It must exist."},
	{Name: "dataset-dir", Value: "<path>", Meaning: "Dataset directory", Section: "§13.1", Scope: ScopeGlobal, Setting: "DATASET_DIR",
		Detail: "Where datasets would be stored. This tool collects none, but the setting is part of the contract."},
	{Name: "timeout", Value: "<duration>", Meaning: "Per-request deadline", Section: "§13.5", Scope: ScopeGlobal, Setting: "TIMEOUT", MCPExtra: true,
		Detail: "Deadline for each upstream request, such as 30s."},
	{Name: "budget", Value: "<duration>", Meaning: "Per-invocation deadline", Section: "§13.5", Scope: ScopeGlobal, Setting: "BUDGET", MCPExtra: true,
		Detail: "Deadline for the whole invocation, including retries and paging. 60s, or 600s with --all."},
	{Name: "timing", Meaning: "Add timing to meta", Section: "§3.2", Scope: ScopeGlobal, MCPExtra: true,
		Detail: "Adds durations and request counts in meta.timing. Off by default so identical calls give identical output."},
	{Name: "deterministic", Meaning: "Deterministic ids and paths", Section: "§14", Scope: ScopeGlobal, MCPExtra: true,
		Detail: "Removes retry jitter. This tool collects no datasets, so dataset ids and paths do not apply."},
	{Name: "verbose", Meaning: "Debug diagnostics on stderr", Section: "§2", Scope: ScopeGlobal,
		Detail: "Sets LOG_LEVEL to debug. Diagnostics go to stderr as JSON lines, redacted."},
	{Name: "schema", Meaning: "Config file schema", Section: "§7.2", Scope: ScopeListConfig,
		Detail: "Returns the JSON Schema of the config file."},
	{Name: "detail", Meaning: "Descriptions in tools", Section: "§6.1", Scope: ScopeTools,
		Detail: "Adds a one-line description and whether each command mutates."},
	{Name: "list", Meaning: "Topic index", Section: "§6.3", Scope: ScopeTeach,
		Detail: "Lists the teach topics, one line each."},
	{Name: "transport", Value: "stdio|http", Meaning: "Server transport", Section: "§16", Scope: ScopeServe,
		Detail: "stdio (default) or http (Streamable HTTP)."},
	{Name: "addr", Value: "<host:port>", Meaning: "Server address", Section: "§16", Scope: ScopeServe,
		Detail: "HTTP listen address. Default 127.0.0.1:7810. A bare port binds to loopback."},
	{Name: "allow-remote", Meaning: "Permit a non-loopback bind", Section: "§16", Scope: ScopeServe,
		Detail: "Required, with a token and a TLS decision, for any address that is not loopback."},
	{Name: "token-file", Value: "<path>", Meaning: "Server bearer token file", Section: "§16", Scope: ScopeServe,
		Detail: "File holding the HTTP bearer token. Must be mode 0600 or 0400."},
	{Name: "tls-cert-file", Value: "<path>", Meaning: "Server TLS certificate", Section: "§16.3", Scope: ScopeServe,
		Detail: "Serve HTTPS with this certificate. Needs --tls-key-file."},
	{Name: "tls-key-file", Value: "<path>", Meaning: "Server TLS private key", Section: "§16.3", Scope: ScopeServe,
		Detail: "Private key for --tls-cert-file. Must be mode 0600 or 0400."},
	{Name: "tls-terminated-upstream", Meaning: "TLS is terminated in front of the tool", Section: "§16.3", Scope: ScopeServe,
		Detail: "Declares that a proxy, ingress, or mesh terminates TLS and the hop to the tool is private. The tool itself then serves plaintext."},
	{Name: "version", Meaning: "Same as the version command", Section: "§18", Scope: ScopeGlobal,
		Detail: "Prints the version envelope."},
	{Name: "help", Meaning: "Help text", Section: "§6.4", Scope: ScopeGlobal,
		Detail: "Human-readable help, ending with a pointer to describe. -h is the same."},
}

func ReservedFlag(name string) *FlagDef {
	for index := range ReservedFlags {
		if ReservedFlags[index].Name == name {
			return &ReservedFlags[index]
		}
	}
	return nil
}

// serveFlags are the only flags serve accepts (§16.1).
var serveFlags = map[string]bool{
	"transport": true, "addr": true, "allow-remote": true, "token-file": true, "tls-cert-file": true,
	"tls-key-file": true, "tls-terminated-upstream": true, "profile": true, "config": true,
	"verbose": true, "deterministic": true, "help": true,
}

// FlagApplies reports whether a reserved flag is accepted by a command.
func FlagApplies(flag *FlagDef, command *Command) bool {
	if command.Name == "serve" {
		return serveFlags[flag.Name]
	}
	switch flag.Scope {
	case ScopeGlobal:
		return true
	case ScopeData:
		return command.Kind == KindObject || command.Kind == KindList
	case ScopePaged:
		return command.Paged
	case ScopeUpstream:
		return command.Kind == KindList
	case ScopeWindow:
		return command.TimeWindow
	case ScopeConfirm:
		return command.IsWrite()
	case ScopeTools:
		return command.Name == "tools"
	case ScopeTeach:
		return command.Name == "teach"
	case ScopeListConfig:
		return command.Name == "list-config"
	case ScopeServe:
		return false
	}
	return false
}
