package app

import (
	"github.com/guyravid/ai/cli/tools/telegram/internal/envelope"
	"github.com/guyravid/ai/cli/tools/telegram/internal/secrets"
	"github.com/guyravid/ai/cli/tools/telegram/internal/teach"
)

// has reports whether a command exists in this build, so the orientation names only commands that
// do (contract §6.3: every command named in teach output exists in tools).
func (a *App) has(name string) bool { return a.Registry != nil && a.Registry.Get(name) != nil }

// orientation is the hand-written part of bare `teach` (base/teach/orientation.md.tmpl). What it says
// about sending, waiting and acknowledging appears only when those commands are in this build.
func (a *App) orientation() teach.Domain {
	domain := teach.Domain{
		Summary: "`telegram` works through one Telegram bot: it shows which bot and chat you are using and reads the incoming messages " +
			"waiting in the bot's update queue.",
		Common: []teach.Common{
			{Question: "Which bot is this?", Command: "bot get"},
			{Question: "What is this chat?", Command: "chats get --chat <id>"},
			{Question: "What has arrived for the bot?", Command: "updates list"},
		},
		Traps: []string{
			"`default` is the PR-assistant bot. Another bot is another profile: choose it with `list-profiles`, then `--profile <name>`. A profile never uses the default bot's token.",
			"Chat ids are strings; groups are negative. `--chat` takes an id or a public `@username`, and without it commands use the profile's `default_chat`.",
			"`updates list` never removes updates. Telegram shows at most 100 unconfirmed ones and drops them after 24 hours; it is a queue, not a message history.",
			"Only one reader of a bot's updates can run at a time; a second one, or a webhook, gives `conflict`.",
			"A chat that never pressed Start on the bot is `chat_unreachable` (exit 32), which only the recipient can fix.",
		},
	}
	if a.has("messages.send") {
		domain.Summary += " It can also send messages and files, and wait for a person's reply."
		domain.Traps = append(domain.Traps,
			"Sending needs `--confirm`; without it the refusal shows the exact request.",
			"Text longer than 4096 characters is rejected as `validation`; keep long text out of argv with `--text-file`.")
	}
	if a.has("updates.wait") && a.has("messages.send") {
		domain.Traps = append(domain.Traps,
			"The quickest flow is `messages send`, then `updates wait --after-message <message_id>` for the reply; `teach delegation` has the worked example.")
	} else if a.has("updates.wait") {
		domain.Traps = append(domain.Traps,
			"`updates wait` returns the next message someone sends the bot, or nothing after `--max-wait`. It never removes updates.")
	}
	if a.has("updates.ack") {
		domain.Traps = append(domain.Traps,
			"`updates ack` permanently deletes updates for every reader of the bot. Use it only when the queue is full.")
	}
	return domain
}

// configNotes are this tool's own rules, added to the generated `teach config` aspects.
func (a *App) configNotes() teach.ConfigNotes {
	return teach.ConfigNotes{
		Credentials: "**`BOT_TOKEN` is a profile-scoped credential (contract §12.1): a selected profile resolves it only from its own scope, never from the default scope.** " +
			"The profile scope is, in this order: `TELEGRAM_BOT_TOKEN_<PROFILE>`, `TELEGRAM_BOT_TOKEN_FILE_<PROFILE>`, the profile's own " +
			"`bot_token_file` (or its `env_file`), and a line keyed `TELEGRAM_BOT_TOKEN_<PROFILE>` in the default section's `env_file`. " +
			"The default scope is `TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_TOKEN_FILE`, the default section's `bot_token_file`, and an " +
			"unsuffixed key in its `env_file`; it is skipped entirely for a selected profile, so a profile uses its own file even " +
			"when `TELEGRAM_BOT_TOKEN_FILE` is set. A profile with nothing in its own scope, where the default scope has a token, " +
			"fails with `config` (exit 3) naming the skipped source; with no token anywhere it is `auth`. Without a profile every " +
			"source is used, in the usual order. `list-config` shows the source actually used, or `set: false` with a hint naming " +
			"the skipped one. The token is sent in the URL path of each request, so it is redacted from every error and log line.",
		Profiles: "**Tool-specific rules.** One profile is one bot, so a profile must have a `bot_token_file` of its own (see " +
			"credentials). Its other settings, including `default_chat` and `allowed_chats`, still inherit from `default`: " +
			"a profile that sets only `bot_token_file` addresses the same chats as the default bot. A private chat id is the " +
			"person's user id and works for every bot, but only after that person pressed Start on that bot. Do not commit " +
			"a config file that holds real chat ids.",
	}
}

func (a *App) teachInput() teach.Input {
	return teach.Input{
		Data: teach.Data{
			Tool: a.Build.Tool, ToolVersion: a.Build.Version, Contract: envelope.ContractVersion, Prefix: a.Build.Prefix,
			HasDatasets: false, WritesEnabled: a.hasWrites(), MCPEnabled: a.Build.MCPEnabled,
			Example:      teach.Example{Name: "updates.list", Argv: "updates list"},
			WriteExample: a.writeExample(),
			Domain:       a.orientation(),
		},
		Registry:    a.Registry,
		Credentials: Credentials,
		Notes:       a.configNotes(),
	}
}

// hasWrites reports whether this build has any mutating command to teach. A full build that has not
// got its write commands yet teaches itself as read-only, since teach must not name what is absent.
func (a *App) hasWrites() bool {
	for _, command := range a.Registry.Commands() {
		if command.IsWrite() {
			return true
		}
	}
	return false
}

// writeExample is a real mutating command for `teach contract`, or empty while the build has none.
func (a *App) writeExample() teach.Example {
	if a.has("messages.send") {
		return teach.Example{Name: "messages.send", Argv: `messages send --text "<text>" --confirm`}
	}
	return teach.Example{}
}

// teach writes Markdown, not an envelope (contract §6.3). Errors are still envelopes.
func (a *App) teach(invocation *Invocation) *Response {
	topic, _ := invocation.Params["topic"].(string)
	item, _ := invocation.Params["item"].(string)
	text, err := teach.Render(a.teachInput(), invocation.Bool("list"), topic, item)
	if err != nil {
		name := invocation.Command.Name
		failed := envelope.New(a.Build.Tool, a.Build.Version, &name)
		failed.SetError(err)
		return &Response{Envelope: failed, Encoded: Encode(failed, invocation.Bool("pretty")), Exit: failed.ExitCode(),
			redactor: secrets.NewRedactor(nil, nil)}
	}
	return &Response{Text: text, Exit: 0, redactor: secrets.NewRedactor(nil, nil)}
}
