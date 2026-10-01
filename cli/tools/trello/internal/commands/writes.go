//go:build !readonly

package commands

import (
	"context"
	"path/filepath"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/trello"
	"github.com/guyravid/ai/cli/tools/trello/internal/upstream"
)

// WritesEnabled reports whether this build includes write commands.
const WritesEnabled = true

var writeErrors = []errs.Code{errs.Usage, errs.Config, errs.Auth, errs.NotFound, errs.Validation, errs.Conflict,
	errs.Refused, errs.RateLimited, errs.Timeout, errs.Network, errs.Upstream}

type cardsCreateInput struct {
	ListID string `json:"list_id" jsonschema:"List to create the card in"`
	Name   string `json:"name" jsonschema:"Card title"`
	Desc   string `json:"desc,omitempty" jsonschema:"Card description (Markdown)"`
	Due    string `json:"due,omitempty" jsonschema:"Due date, RFC 3339"`
	Pos    string `json:"pos,omitempty" jsonschema:"top, bottom, or a positive number"`
}

type cardsMoveInput struct {
	ID     string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	ListID string `json:"list_id" jsonschema:"Destination list"`
	Pos    string `json:"pos,omitempty" jsonschema:"top or bottom of the destination list"`
}

type cardsUpdateInput struct {
	ID   string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	Desc string `json:"desc" jsonschema:"New description (Markdown); replaces the current one"`
}

type cardsCommentInput struct {
	ID   string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	Text string `json:"text" jsonschema:"Comment text (Markdown)"`
}

type attachURLInput struct {
	ID   string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	URL  string `json:"url" jsonschema:"Link to attach"`
	Name string `json:"name,omitempty" jsonschema:"Attachment name shown on the card"`
}

type attachFileInput struct {
	ID   string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	File string `json:"file" jsonschema:"Local file to upload"`
	Name string `json:"name,omitempty" jsonschema:"Attachment name; defaults to the file name"`
}

// body builds a request body from key/value pairs, leaving out empty values.
func body(pairs ...string) map[string]string {
	values := map[string]string{}
	for index := 0; index+1 < len(pairs); index += 2 {
		if pairs[index+1] != "" {
			values[pairs[index]] = pairs[index+1]
		}
	}
	return values
}

// Writes returns the mutating domain commands. They are compiled out of read-only builds.
func Writes() []*registry.Command {
	return []*registry.Command{
		registry.Write[cardsCreateInput, trello.Card](registry.Spec{
			Name:        "cards.create",
			Description: "Create a card in a list.",
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "create", "--list-id", "61a0c3", "--name", "Fix paging", "--confirm"},
				Description: "Create a card"}},
		}, func(ctx context.Context, call *registry.Call, in cardsCreateInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "POST", Path: "/cards",
				Body: body("idList", in.ListID, "name", in.Name, "desc", in.Desc, "due", in.Due, "pos", in.Pos)}, nil
		}),

		registry.Write[cardsMoveInput, trello.Card](registry.Spec{
			Name:        "cards.move",
			Description: "Move a card to another list.",
			Idempotent:  true,
			Enums:       map[string][]string{"pos": {"top", "bottom"}},
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "move", "91bc4d", "--list-id", "61a0c3", "--confirm"},
				Description: "Move a card"}},
		}, func(ctx context.Context, call *registry.Call, in cardsMoveInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "PUT", Path: resource("cards", in.ID), Body: body("idList", in.ListID, "pos", in.Pos)}, nil
		}),

		registry.Write[cardsUpdateInput, trello.Card](registry.Spec{
			Name:        "cards.update",
			Description: "Replace a card's description.",
			Destructive: true,
			Idempotent:  true,
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "update", "91bc4d", "--desc", "Steps to reproduce…", "--confirm"},
				Description: "Replace the description"}},
		}, func(ctx context.Context, call *registry.Call, in cardsUpdateInput) (*upstream.Request, error) {
			// Sent even when empty: clearing a description is a legitimate update.
			return &upstream.Request{Method: "PUT", Path: resource("cards", in.ID), Body: map[string]string{"desc": in.Desc}}, nil
		}),

		registry.Write[cardsCommentInput, trello.Action](registry.Spec{
			Name:        "cards.comment",
			Description: "Add a comment to a card.",
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "comment", "91bc4d", "--text", "Fixed in 0.3.1", "--confirm"},
				Description: "Comment on a card"}},
		}, func(ctx context.Context, call *registry.Call, in cardsCommentInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "POST", Path: resource("cards", in.ID) + "/actions/comments", Body: body("text", in.Text)}, nil
		}),

		registry.Write[attachURLInput, trello.Attachment](registry.Spec{
			Name:        "cards.attach-url",
			Description: "Attach a link to a card.",
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "attach-url", "91bc4d", "--url", "https://example.com/spec", "--confirm"},
				Description: "Attach a link"}},
		}, func(ctx context.Context, call *registry.Call, in attachURLInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "POST", Path: resource("cards", in.ID) + "/attachments", Body: body("url", in.URL, "name", in.Name)}, nil
		}),

		registry.Write[attachFileInput, trello.Attachment](registry.Spec{
			Name:        "cards.attach-file",
			Description: "Upload a local file to a card as an attachment.",
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "attach-file", "91bc4d", "--file", "./report.pdf", "--confirm"},
				Description: "Upload a file"}},
		}, func(ctx context.Context, call *registry.Call, in attachFileInput) (*upstream.Request, error) {
			resolved := resolvePath(in.File)
			name := in.Name
			if name == "" {
				name = filepath.Base(resolved)
			}
			// The file is checked when sending, not here, so the preview always shows what
			// would be sent (base template §13).
			return &upstream.Request{Method: "POST", Path: resource("cards", in.ID) + "/attachments",
				Upload: &upstream.Upload{Field: "file", Path: resolved, Name: name, Fields: body("name", name)}}, nil
		}),
	}
}

func resolvePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	return absolute
}
