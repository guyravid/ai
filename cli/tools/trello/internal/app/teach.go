package app

import (
	"github.com/guyravid/ai/cli/tools/trello/internal/envelope"
	"github.com/guyravid/ai/cli/tools/trello/internal/secrets"
	"github.com/guyravid/ai/cli/tools/trello/internal/teach"
)

// orientation is the hand-written part of bare `teach` (base/teach/orientation.md.tmpl).
func (a *App) orientation() teach.Domain {
	domain := teach.Domain{
		Summary: "`trello` reads Trello boards, lists, cards, and board activity through the Trello REST API. " +
			"Use it to find boards and cards, read a card in full, search across boards, and follow what changed recently.",
		Common: []teach.Common{
			{Question: "Which boards can I see?", Command: "boards list"},
			{Question: "What columns does a board have?", Command: "lists list --board <board>"},
			{Question: "What is on a board?", Command: "cards list --board <board>"},
			{Question: "What does this card say?", Command: "cards get <card>"},
			{Question: "Where is the card about X?", Command: `cards search --query "<words>"`},
			{Question: "What changed recently?", Command: "actions list --board <board> --since -24h"},
		},
		Traps: []string{
			"Boards and cards take either the 24-character id or the short link from their URL (`trello.com/c/<shortLink>`). Lists need the full id.",
			"Filter cards to one list with `--list-id`; `--list` is a reserved flag of `teach`.",
			"`actions list` looks at the last 24 hours unless you pass `--since`; check `meta.window.source`.",
			"`cards list` omits descriptions by default. `cards get <card>` shows them.",
			"`cards search` returns at most 1000 matches, ordered by recent activity, not relevance.",
		},
	}
	if a.Build.WritesEnabled {
		domain.Summary += " With `--confirm` it can also create, move, comment on, archive, and delete cards, replace descriptions, and attach links or files."
		domain.Traps = append(domain.Traps,
			"`cards update` replaces the whole description. Read it with `cards get` first if you mean to append.",
			"`cards delete` is permanent, with the card's comments and attachments. Use `cards archive` unless deletion was asked for.",
			"Commands that change Trello are marked `mutates` in `tools --detail` and never run without `--confirm`.")
	}
	return domain
}

func (a *App) teachInput() teach.Input {
	input := teach.Input{
		Data: teach.Data{
			Tool: a.Build.Tool, ToolVersion: a.Build.Version, Contract: envelope.ContractVersion, Prefix: a.Build.Prefix,
			HasDatasets: true, WritesEnabled: a.Build.WritesEnabled, MCPEnabled: a.Build.MCPEnabled,
			Example: teach.Example{Name: "cards.list", Argv: "cards list --board <board>"},
			Domain:  a.orientation(),
		},
		Registry:    a.Registry,
		Credentials: Credentials,
	}
	if a.Build.WritesEnabled {
		input.Data.WriteExample = teach.Example{Name: "cards.create", Argv: "cards create --list-id <list> --name <name> --confirm"}
	}
	return input
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
