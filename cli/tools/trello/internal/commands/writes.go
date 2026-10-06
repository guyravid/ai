//go:build !readonly

package commands

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"

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

type cardsRenameInput struct {
	ID   string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	Name string `json:"name" jsonschema:"New card title; one line, not empty"`
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

type cardIDInput struct {
	ID string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
}

type attachFileInput struct {
	ID   string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	File string `json:"file" jsonschema:"Local file to upload"`
	Name string `json:"name,omitempty" jsonschema:"Attachment name; defaults to the file name"`
}

type detachInput struct {
	ID         string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	Attachment string `json:"attachment" jsonschema:"Attachment id, from cards attachments"`
}

type addChecklistInput struct {
	ID   string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	Name string `json:"name" jsonschema:"Checklist name"`
}

type addItemInput struct {
	ID      string `json:"id" jsonschema:"Checklist id, from cards checklists" cli:"positional"`
	Name    string `json:"name" jsonschema:"Check item text"`
	Checked bool   `json:"checked,omitempty" jsonschema:"Create the item already complete"`
}

type checkItemInput struct {
	ID        string `json:"id" jsonschema:"Card id or short link" cli:"positional"`
	CheckItem string `json:"check_item" jsonschema:"Check item id, from cards checklists"`
	State     string `json:"state" jsonschema:"complete or incomplete"`
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

		registry.Write[cardsRenameInput, trello.Card](registry.Spec{
			Name:        "cards.rename",
			Description: "Change a card's title.",
			Idempotent:  true,
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "rename", "91bc4d", "--name", "Fix paging bug", "--confirm"},
				Description: "Rename a card"}},
		}, func(ctx context.Context, call *registry.Call, in cardsRenameInput) (*upstream.Request, error) {
			if strings.TrimSpace(in.Name) == "" {
				return nil, errs.New(errs.Validation, "The card name must not be empty.")
			}
			if strings.ContainsAny(in.Name, "\r\n") {
				return nil, errs.New(errs.Validation, "The card name must be a single line.")
			}
			return &upstream.Request{Method: "PUT", Path: resource("cards", in.ID), Body: map[string]string{"name": in.Name}}, nil
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

		registry.Write[cardIDInput, trello.Card](registry.Spec{
			Name:        "cards.archive",
			Description: "Archive a card. Reversible in Trello; prefer it to delete.",
			Idempotent:  true,
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "archive", "91bc4d", "--confirm"},
				Description: "Archive a card"}},
		}, func(ctx context.Context, call *registry.Call, in cardIDInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "PUT", Path: resource("cards", in.ID), Body: body("closed", "true")}, nil
		}),

		// Trello answers a delete with no card, only an empty limits object, so data is raw JSON.
		registry.Write[cardIDInput, json.RawMessage](registry.Spec{
			Name:        "cards.delete",
			Description: "Permanently delete a card, with its comments and attachments. Cannot be undone.",
			Destructive: true,
			Idempotent:  true,
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "delete", "91bc4d", "--confirm"},
				Description: "Delete a card for good"}},
		}, func(ctx context.Context, call *registry.Call, in cardIDInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "DELETE", Path: resource("cards", in.ID)}, nil
		}),

		registry.Write[addChecklistInput, trello.Checklist](registry.Spec{
			Name:        "cards.add-checklist",
			Description: "Create an empty checklist on a card.",
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "add-checklist", "91bc4d", "--name", "Release steps", "--confirm"},
				Description: "Add a checklist"}},
		}, func(ctx context.Context, call *registry.Call, in addChecklistInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "POST", Path: resource("cards", in.ID) + "/checklists", Body: body("name", in.Name)}, nil
		}),

		registry.Write[addItemInput, trello.CheckItem](registry.Spec{
			Name:        "checklists.add-item",
			Description: "Add an item to a checklist.",
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"checklists", "add-item", "62d0a1", "--name", "Tag the release", "--confirm"},
				Description: "Add an item"}},
		}, func(ctx context.Context, call *registry.Call, in addItemInput) (*upstream.Request, error) {
			checked := ""
			if in.Checked {
				checked = "true"
			}
			return &upstream.Request{Method: "POST", Path: resource("checklists", in.ID) + "/checkItems",
				Body: body("name", in.Name, "checked", checked)}, nil
		}),

		registry.Write[checkItemInput, trello.CheckItem](registry.Spec{
			Name:        "cards.check-item",
			Description: "Mark a check item complete or incomplete.",
			Idempotent:  true,
			Enums:       map[string][]string{"state": {"complete", "incomplete"}},
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "check-item", "91bc4d", "--check-item", "63e1b2", "--state", "complete", "--confirm"},
				Description: "Tick an item"}},
		}, func(ctx context.Context, call *registry.Call, in checkItemInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "PUT", Path: resource("cards", in.ID) + "/checkItem/" + url.PathEscape(in.CheckItem),
				Body: body("state", in.State)}, nil
		}),

		// Trello answers an attachment delete with {"_value":null}, so data is raw JSON.
		registry.Write[detachInput, json.RawMessage](registry.Spec{
			Name:        "cards.detach",
			Description: "Remove an attachment from a card. Uploaded files are deleted from Trello. Cannot be undone.",
			Destructive: true,
			Idempotent:  true,
			Errors:      writeErrors,
			Examples: []registry.Example{{Argv: []string{"cards", "detach", "91bc4d", "--attachment", "64f2c3", "--confirm"},
				Description: "Remove an attachment"}},
		}, func(ctx context.Context, call *registry.Call, in detachInput) (*upstream.Request, error) {
			return &upstream.Request{Method: "DELETE", Path: resource("cards", in.ID) + "/attachments/" + url.PathEscape(in.Attachment)}, nil
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
