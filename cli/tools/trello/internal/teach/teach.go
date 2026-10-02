// Package teach renders the tool's Markdown guidance (contract §6.3). The orientation and contract
// pages are vendored templates; flags and config are generated from the same definitions as
// describe and list-config; domain topics are compiled in.
package teach

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"text/template"

	"github.com/guyravid/ai/cli/tools/trello/internal/config"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

//go:embed orientation.md.tmpl
var orientationTemplate string

//go:embed shared.md.tmpl
var sharedTemplate string

//go:embed topics/*.md
var topicFiles embed.FS

type Example struct {
	Name string
	Argv string
}

type Common struct {
	Question string
	Command  string
}

type Domain struct {
	Summary string
	Common  []Common
	Traps   []string
}

// Data is the template data both vendored templates expect.
type Data struct {
	Tool          string
	ToolVersion   string
	Contract      string
	Prefix        string
	HasDatasets   bool
	WritesEnabled bool
	MCPEnabled    bool
	Example       Example
	WriteExample  Example // a real mutating command; set only when WritesEnabled
	Domain        Domain
}

type Input struct {
	Data        Data
	Registry    *registry.Registry
	Credentials []string
}

// reservedTopics may not be defined by a tool author (contract §6.3).
var reservedTopics = map[string]bool{"contract": true, "flags": true, "config": true}

type topic struct {
	name    string
	summary string
	body    string
	writes  bool
}

func domainTopics(writesEnabled bool) ([]topic, error) {
	entries, err := fs.ReadDir(topicFiles, "topics")
	if err != nil {
		return nil, err
	}
	var topics []topic
	for _, entry := range entries {
		content, err := topicFiles.ReadFile("topics/" + entry.Name())
		if err != nil {
			return nil, err
		}
		parsed, err := parseTopic(strings.TrimSuffix(entry.Name(), ".md"), string(content))
		if err != nil {
			return nil, err
		}
		if reservedTopics[parsed.name] {
			return nil, fmt.Errorf("topic %q is reserved", parsed.name)
		}
		if parsed.writes && !writesEnabled {
			continue
		}
		topics = append(topics, parsed)
	}
	return topics, nil
}

// parseTopic reads the front matter: "summary:" (one line) and optional "writes: true".
func parseTopic(name, content string) (topic, error) {
	parsed := topic{name: name}
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return parsed, fmt.Errorf("topic %s has no front matter", name)
	}
	header, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return parsed, fmt.Errorf("topic %s has unterminated front matter", name)
	}
	for _, line := range strings.Split(header, "\n") {
		key, value, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(key) {
		case "summary":
			parsed.summary = strings.TrimSpace(value)
		case "writes":
			parsed.writes = strings.TrimSpace(value) == "true"
		}
	}
	parsed.body = strings.TrimLeft(body, "\n")
	return parsed, nil
}

// Render answers `teach [topic [item]]` and `teach --list`.
func Render(in Input, list bool, topicName, item string) (string, *errs.Error) {
	topics, err := domainTopics(in.Data.WritesEnabled)
	if err != nil {
		return "", errs.New(errs.Internal, "The compiled-in teach topics are invalid: %s.", err.Error())
	}
	if list {
		return renderList(in, topics), nil
	}
	switch topicName {
	case "":
		return execute("orientation", orientationTemplate, in.Data)
	case "contract":
		if item != "" {
			return "", errs.Usagef("teach contract has no items; it is read in one piece.").WithHint(in.Data.Tool + " teach contract")
		}
		return execute("contract", sharedTemplate, in.Data)
	case "flags":
		return renderFlags(in, item)
	case "config":
		return renderConfig(in, item)
	}
	for _, candidate := range topics {
		if candidate.name == topicName {
			return renderDomainTopic(in, candidate, item)
		}
	}
	names := []string{"contract", "flags", "config"}
	for _, candidate := range topics {
		names = append(names, candidate.name)
	}
	failure := errs.Usagef("No teach topic named %q.", topicName).
		WithHint(in.Data.Tool+" teach --list").WithDetail("topic", topicName)
	if suggestion := shape.Closest(topicName, names); suggestion != "" {
		failure.WithDetail("did_you_mean", suggestion)
	}
	return "", failure
}

func execute(name, text string, data Data) (string, *errs.Error) {
	parsed, err := template.New(name).Parse(text)
	if err != nil {
		return "", errs.New(errs.Internal, "The %s template does not parse.", name)
	}
	var buffer bytes.Buffer
	if err := parsed.Execute(&buffer, data); err != nil {
		return "", errs.New(errs.Internal, "The %s template failed to render.", name)
	}
	return strings.TrimRight(buffer.String(), "\n") + "\n", nil
}

func renderList(in Input, topics []topic) string {
	tool := in.Data.Tool
	var builder strings.Builder
	fmt.Fprintf(&builder, "# %s teach topics\n\n", tool)
	fmt.Fprintf(&builder, "- `%s teach` — orientation: start here\n", tool)
	fmt.Fprintf(&builder, "- `%s teach contract` — rules shared by every tool in this family\n", tool)
	fmt.Fprintf(&builder, "- `%s teach flags` — every reserved flag; `teach flags <name>` for one\n", tool)
	fmt.Fprintf(&builder, "- `%s teach config` — settings, precedence, profiles, files, credentials, example file\n", tool)
	for _, candidate := range topics {
		fmt.Fprintf(&builder, "- `%s teach %s` — %s\n", tool, candidate.name, candidate.summary)
	}
	return builder.String()
}

// renderDomainTopic returns a topic, or one "## " section of it when item is given.
func renderDomainTopic(in Input, candidate topic, item string) (string, *errs.Error) {
	body := strings.ReplaceAll(candidate.body, "{{tool}}", in.Data.Tool)
	if item == "" {
		return fmt.Sprintf("# %s\n\n%s", candidate.summary, body), nil
	}
	sections := splitSections(body)
	var slugs []string
	for _, section := range sections {
		slugs = append(slugs, section.slug)
		if section.slug == item {
			return section.text, nil
		}
	}
	failure := errs.Usagef("Topic %s has no item %q.", candidate.name, item).
		WithHint(fmt.Sprintf("%s teach %s", in.Data.Tool, candidate.name)).WithDetail("items", slugs)
	if suggestion := shape.Closest(item, slugs); suggestion != "" {
		failure.WithDetail("did_you_mean", suggestion)
	}
	return "", failure
}

type section struct {
	slug string
	text string
}

func splitSections(body string) []section {
	var sections []section
	for _, chunk := range strings.Split("\n"+body, "\n## ")[1:] {
		title, _, _ := strings.Cut(chunk, "\n")
		sections = append(sections, section{slug: slugify(title), text: "## " + strings.TrimRight(chunk, "\n") + "\n"})
	}
	return sections
}

func slugify(title string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == ' ' || r == '-':
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func renderFlags(in Input, item string) (string, *errs.Error) {
	var applicable []registry.FlagDef
	for _, flag := range registry.ReservedFlags {
		if flag.Scope == registry.ScopeServe && !in.Data.MCPEnabled {
			continue
		}
		if flag.Scope == registry.ScopeConfirm && !in.Data.WritesEnabled {
			flag.Scope = "dataset clear"
		}
		applicable = append(applicable, flag)
	}
	if item != "" {
		name := strings.TrimLeft(item, "-")
		for _, flag := range applicable {
			if flag.Name == name {
				return flagDetail(in, flag), nil
			}
		}
		var names []string
		for _, flag := range applicable {
			names = append(names, flag.Name)
		}
		failure := errs.Usagef("No reserved flag --%s in this build.", name).WithHint(in.Data.Tool + " teach flags")
		if suggestion := shape.Closest(name, names); suggestion != "" {
			failure.WithDetail("did_you_mean", suggestion)
		}
		return "", failure
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Flags of %s\n\n", in.Data.Tool)
	builder.WriteString("Every reserved flag this build supports, with one meaning everywhere. Command-specific flags are in `describe <name>`.\n\n")
	builder.WriteString("| Flag | Meaning | Applies to |\n|---|---|---|\n")
	for _, flag := range applicable {
		usage := "--" + flag.Name
		if flag.Value != "" {
			usage += " " + flag.Value
		}
		fmt.Fprintf(&builder, "| `%s` | %s | %s |\n", usage, flag.Meaning, flag.Scope)
	}
	builder.WriteString("| `-h` | Help text | every command |\n")
	fmt.Fprintf(&builder, "\nNever accepted: %s. A credential on the command line is refused and must be rotated.\n\n",
		"`--"+strings.Join(registry.ForbiddenFlags, "`, `--")+"`")
	fmt.Fprintf(&builder, "One flag in full: `%s teach flags <name>`.\n", in.Data.Tool)
	return builder.String(), nil
}

func flagDetail(in Input, flag registry.FlagDef) string {
	var builder strings.Builder
	usage := "--" + flag.Name
	if flag.Value != "" {
		usage += " " + flag.Value
	}
	fmt.Fprintf(&builder, "# `%s`\n\n%s\n\n", usage, flag.Detail)
	fmt.Fprintf(&builder, "- Applies to: %s\n", flag.Scope)
	if flag.Setting != "" {
		fmt.Fprintf(&builder, "- Setting: `%s` (environment `%s_%s`)\n", flag.Setting, in.Data.Prefix, flag.Setting)
	}
	if flag.MCPExtra && in.Data.MCPEnabled {
		fmt.Fprintf(&builder, "- Over MCP: argument `%s`\n", strings.ReplaceAll(flag.Name, "-", "_"))
	}
	fmt.Fprintf(&builder, "- Contract: %s\n", flag.Section)
	return builder.String()
}

// configAspects are the parts of `teach config` that can be opened alone.
var configAspects = []string{"settings", "precedence", "profiles", "files", "credentials", "example"}

func renderConfig(in Input, aspect string) (string, *errs.Error) {
	parts := map[string]string{
		"settings":    configSettings(in),
		"precedence":  configPrecedence(in),
		"profiles":    configProfiles(in),
		"files":       configFiles(in),
		"credentials": configCredentials(in),
		"example":     configExample(in),
	}
	if aspect != "" {
		if text, ok := parts[aspect]; ok {
			return text, nil
		}
		failure := errs.Usagef("teach config has no aspect %q.", aspect).
			WithHint(in.Data.Tool+" teach config").WithDetail("aspects", configAspects)
		if suggestion := shape.Closest(aspect, configAspects); suggestion != "" {
			failure.WithDetail("did_you_mean", suggestion)
		}
		return "", failure
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Configuring %s\n\n", in.Data.Tool)
	fmt.Fprintf(&builder, "Each part can be opened alone: `%s teach config <aspect>`, where aspect is one of %s.\n\n",
		in.Data.Tool, strings.Join(configAspects, ", "))
	for _, aspect := range configAspects {
		builder.WriteString(parts[aspect])
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n") + "\n", nil
}

func configSettings(in Input) string {
	var builder strings.Builder
	builder.WriteString("## Settings\n\n| Setting | Type | Default | Environment | Flag | Config file member |\n|---|---|---|---|---|---|\n")
	for _, def := range config.Defs() {
		defaultText := "platform default"
		if def.Default != nil {
			defaultText = fmt.Sprint(def.Default)
		} else if def.Name == "PROFILE" {
			defaultText = "none"
		}
		flag, member := "—", "—"
		if def.Flag != "" {
			flag = "`" + def.Flag + "`"
		}
		if def.InFile {
			member = "`" + def.FileKey() + "`"
		}
		fmt.Fprintf(&builder, "| `%s` | %s | %s | `%s_%s` | %s | %s |\n", def.Name, def.Kind, defaultText, in.Data.Prefix, def.Name, flag, member)
	}
	for _, name := range in.Credentials {
		fmt.Fprintf(&builder, "| `%s` | credential | none | `%s_%s` or `%s_%s_FILE` | never | `%s_file` (a path) |\n",
			name, in.Data.Prefix, name, in.Data.Prefix, name, strings.ToLower(name))
	}
	builder.WriteString("\nDurations and sizes carry units (`30s`, `4h`, `7d`, `256MB`, `2GiB`); a bare number is rejected.\n")
	builder.WriteString("Every setting is also read from `AGENTCLI_<SETTING>`, shared by every tool in this family.\n")
	return builder.String()
}

func configPrecedence(in Input) string {
	return fmt.Sprintf(`## Precedence

Each setting resolves on its own; the first match wins:

1. The command-line flag.
2. %[1]s_<SETTING>_<PROFILE>, when a profile is active.
3. %[1]s_<SETTING>.
4. The config file, profiles.<profile>.<setting>.
5. The config file, default.<setting>.
6. AGENTCLI_<SETTING>.
7. The built-in default.

The environment outranks the file, so a deployment can override a file it does not own. `+"`%[2]s list-config`"+` shows the exact source of every value.
`, in.Data.Prefix, in.Data.Tool)
}

func configProfiles(in Input) string {
	return fmt.Sprintf(`## Profiles

A profile is a named, partial set of overrides. Select one with `+"`--profile <name>`"+` or %[1]s_PROFILE; the flag wins.

- A profile changes only the settings it defines. Everything else keeps its value from the tiers below.
- A profile exists when the config file declares it under "profiles", or when any %[1]s_<SETTING>_<PROFILE> variable is set for it. Selecting any other name is a config error.
- In variable names the profile is uppercased, with - replaced by _: profile eu-west reads %[1]s_TIMEOUT_EU_WEST.
- A profile cannot be named FILE or default.
- `+"`%[2]s list-profiles`"+` lists the profiles you can use: `+"`default`"+` (no `+"`--profile`"+`) first, then each declared profile. An empty list means there are none, so leave `+"`--profile`"+` off.
`, in.Data.Prefix, in.Data.Tool)
}

func configFiles(in Input) string {
	return fmt.Sprintf(`## Files

| What | Location |
|---|---|
| Config file | <config base>/agentcli/%[1]s/config.json |
| Datasets | <cache base>/agentcli/%[1]s/datasets/ |

| Platform | Config base | Cache base |
|---|---|---|
| Linux and other Unix | $XDG_CONFIG_HOME, else ~/.config | $XDG_CACHE_HOME, else ~/.cache |
| macOS | $XDG_CONFIG_HOME if set, else ~/Library/Application Support | $XDG_CACHE_HOME if set, else ~/Library/Caches |
| Windows | %%APPDATA%% | %%LOCALAPPDATA%% |

The config file at that location loads automatically when present. `+"`--config <path>`"+` or %[2]s_CONFIG names another file, which must exist. The tool never writes or creates the config file. If no home directory can be determined, set %[2]s_CONFIG and %[2]s_DATASET_DIR; the tool will not guess.
`, in.Data.Tool, in.Data.Prefix)
}

func configCredentials(in Input) string {
	var builder strings.Builder
	builder.WriteString("## Credentials\n\n")
	fmt.Fprintf(&builder, "This tool needs %s. Never pass them as arguments.\n\n", strings.Join(in.Credentials, " and "))
	builder.WriteString("For each credential, the first match wins:\n\n")
	fmt.Fprintf(&builder, "1. `%[1]s_<NAME>_<PROFILE>` — the value, profile-scoped.\n", in.Data.Prefix)
	fmt.Fprintf(&builder, "2. `%[1]s_<NAME>_FILE_<PROFILE>` — a path to a file holding the value, profile-scoped.\n", in.Data.Prefix)
	fmt.Fprintf(&builder, "3. `%[1]s_<NAME>` — the value.\n", in.Data.Prefix)
	fmt.Fprintf(&builder, "4. `%[1]s_<NAME>_FILE` — a path to a file holding the value.\n", in.Data.Prefix)
	builder.WriteString("5. The config file's active profile: `<name>_file`, or an `env_file`.\n")
	builder.WriteString("6. The config file's `default` section, the same forms.\n\n")
	builder.WriteString("**Credential entries in the config file are paths, never values.** A file that holds a credential value is refused at startup, so the config file is always safe to read or commit.\n\n")
	fmt.Fprintf(&builder, "An `env_file` holds `KEY=value` lines. Only `%[1]s_<NAME>_<PROFILE>` and `%[1]s_<NAME>` keys are read; every other line is ignored.\n\n", in.Data.Prefix)
	builder.WriteString("Every credential file must be mode 0600 or 0400 (`chmod 600 <file>`); anything looser is a config error.\n")
	return builder.String()
}

func configExample(in Input) string {
	return fmt.Sprintf("## Example config file\n\n```json\n%s\n```\n\nProfile `work` overrides two settings and one credential path; everything else comes from `default`.\n",
		strings.TrimSpace(ExampleConfig))
}

// ExampleConfig is a complete config file with a profile that sets more than one value.
const ExampleConfig = `{
  "config_version": 1,
  "default": {
    "limit": 25,
    "timeout": "30s",
    "env_file": "~/.secrets/trello.env"
  },
  "profiles": {
    "work": {
      "dataset_ttl": "4h",
      "max_pages": 20,
      "api_token_file": "~/.secrets/trello-work.token"
    },
    "personal": {
      "api_key_file": "~/.secrets/trello-personal.key",
      "api_token_file": "~/.secrets/trello-personal.token"
    }
  }
}`

// TopicNames returns every topic name, for validation and tests.
func TopicNames(writesEnabled bool) []string {
	topics, _ := domainTopics(writesEnabled)
	names := []string{"contract", "flags", "config"}
	for _, candidate := range topics {
		names = append(names, candidate.name)
	}
	sort.Strings(names[3:])
	return names
}
