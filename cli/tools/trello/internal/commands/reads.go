// Package commands defines the Trello domain commands as registry entries.
package commands

import (
	"context"
	"net/url"
	"strconv"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
	"github.com/guyravid/ai/cli/tools/trello/internal/trello"
	"github.com/guyravid/ai/cli/tools/trello/internal/upstream"
)

var (
	readErrors   = []errs.Code{errs.Usage, errs.Config, errs.Auth, errs.NotFound, errs.RateLimited, errs.Timeout, errs.Network, errs.Upstream}
	listErrors   = append(append([]errs.Code(nil), readErrors...), errs.Partial)
	filterValues = []string{"open", "closed", "all"}
)

type boardsListInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"open (default), closed, or all"`
}

type idInput struct {
	ID string `json:"id" jsonschema:"Trello id or short link" cli:"positional"`
}

type listsListInput struct {
	Board  string `json:"board" jsonschema:"Board id or short link"`
	Filter string `json:"filter,omitempty" jsonschema:"open (default), closed, or all"`
}

type cardsListInput struct {
	Board  string `json:"board" jsonschema:"Board id or short link"`
	ListID string `json:"list_id,omitempty" jsonschema:"Only cards in this list"`
}

type cardsSearchInput struct {
	Query string `json:"query" jsonschema:"Trello search query, such as 'is:open label:bug'"`
	Board string `json:"board,omitempty" jsonschema:"Only cards on this board id"`
}

type actionsListInput struct {
	Board string `json:"board" jsonschema:"Board id or short link"`
	Type  string `json:"type,omitempty" jsonschema:"Comma-separated action types, such as commentCard,updateCard"`
}

// ActionsPageSize is the page size requested from /boards/{id}/actions: Trello's maximum. Tests
// lower it to exercise multi-page filling against a small fake upstream.
var ActionsPageSize = trello.MaxActionsPage

// MaxSearchResults is the most cards one Trello search returns (cards_limit).
const MaxSearchResults = 1000

// resource returns "/<kind>/<escaped id>".
func resource(kind, id string) string { return "/" + kind + "/" + url.PathEscape(id) }

func filterQuery(filter string) url.Values {
	if filter == "" {
		filter = "open"
	}
	return url.Values{"filter": {filter}}
}

// Reads returns the read-only domain commands.
func Reads() []*registry.Command {
	return []*registry.Command{
		registry.List[boardsListInput, trello.Board](registry.Spec{
			Name:        "boards.list",
			Description: "List boards visible to this token, by name.",
			Collectable: true,
			Fields:      &registry.FieldSet{Default: trello.BoardDefault, Available: trello.BoardAvailable},
			Sort:        "name asc, id asc",
			Limits:      &registry.Limits{Default: 25, Max: 200},
			Enums:       map[string][]string{"filter": filterValues},
			Errors:      listErrors,
			Examples: []registry.Example{
				{Argv: []string{"boards", "list"}, Description: "Open boards"},
				{Argv: []string{"boards", "list", "--filter", "all", "--fields", "id,name,closed"}, Description: "Every board, including closed ones"},
			},
		}, func(ctx context.Context, call *registry.Call, in boardsListInput) (registry.ListSource, error) {
			return &trello.LocalList{Sort: call.Sort,
				Load: trello.ArrayLoader(call.Upstream, "/members/me/boards", filterQuery(in.Filter), "boards")}, nil
		}),

		registry.Object[idInput, trello.Board](registry.Spec{
			Name:        "boards.get",
			Description: "Get one board by id or short link.",
			Fields:      &registry.FieldSet{Default: trello.BoardAvailable, Available: trello.BoardAvailable},
			Errors:      readErrors,
			Examples:    []registry.Example{{Argv: []string{"boards", "get", "5f2a1b"}, Description: "One board"}},
		}, func(ctx context.Context, call *registry.Call, in idInput) (shape.Value, error) {
			return call.Upstream.Do(ctx, &upstream.Request{Method: "GET", Path: resource("boards", in.ID)})
		}),

		registry.List[listsListInput, trello.List](registry.Spec{
			Name:        "lists.list",
			Description: "List the lists on a board, in board order.",
			Collectable: true,
			Fields:      &registry.FieldSet{Default: trello.ListDefault, Available: trello.ListAvailable},
			Sort:        "pos asc, id asc",
			Limits:      &registry.Limits{Default: 25, Max: 200},
			Enums:       map[string][]string{"filter": filterValues},
			Errors:      listErrors,
			Examples:    []registry.Example{{Argv: []string{"lists", "list", "--board", "5f2a1b"}, Description: "Columns of a board"}},
		}, func(ctx context.Context, call *registry.Call, in listsListInput) (registry.ListSource, error) {
			return &trello.LocalList{Sort: call.Sort,
				Load: trello.ArrayLoader(call.Upstream, resource("boards", in.Board)+"/lists", filterQuery(in.Filter), "lists")}, nil
		}),

		registry.List[cardsListInput, trello.Card](registry.Spec{
			Name:        "cards.list",
			Description: "List cards on a board, most recently active first.",
			Collectable: true,
			Fields:      &registry.FieldSet{Default: trello.CardDefault, Available: trello.CardAvailable},
			Sort:        "dateLastActivity desc, id asc",
			Limits:      &registry.Limits{Default: 25, Max: 1000},
			Errors:      listErrors,
			Examples: []registry.Example{
				{Argv: []string{"cards", "list", "--board", "5f2a1b"}, Description: "Recently active cards"},
				{Argv: []string{"cards", "list", "--board", "5f2a1b", "--list-id", "61a0c3", "--fields", "id,name,due"}, Description: "Cards in one list, with due dates"},
			},
		}, func(ctx context.Context, call *registry.Call, in cardsListInput) (registry.ListSource, error) {
			load := trello.ArrayLoader(call.Upstream, resource("boards", in.Board)+"/cards", nil, "cards")
			if in.ListID != "" {
				load = filterByField(load, "idList", in.ListID)
			}
			return &trello.LocalList{Sort: call.Sort, Load: load}, nil
		}),

		registry.Object[idInput, trello.Card](registry.Spec{
			Name:        "cards.get",
			Description: "Get one card by id or short link, including its description.",
			Fields:      &registry.FieldSet{Default: trello.CardGetDefault, Available: trello.CardAvailable},
			Errors:      readErrors,
			Examples:    []registry.Example{{Argv: []string{"cards", "get", "91bc4d"}, Description: "One card"}},
		}, func(ctx context.Context, call *registry.Call, in idInput) (shape.Value, error) {
			return call.Upstream.Do(ctx, &upstream.Request{Method: "GET", Path: resource("cards", in.ID)})
		}),

		registry.List[cardsSearchInput, trello.Card](registry.Spec{
			Name:        "cards.search",
			Description: "Search cards across boards; up to 1000 matches, most recently active first.",
			Collectable: true,
			Fields:      &registry.FieldSet{Default: trello.CardDefault, Available: trello.CardAvailable},
			Sort:        "dateLastActivity desc, id asc",
			Limits:      &registry.Limits{Default: 25, Max: 1000},
			Errors:      listErrors,
			Examples: []registry.Example{
				{Argv: []string{"cards", "search", "--query", "paging"}, Description: "Cards mentioning paging"},
				{Argv: []string{"cards", "search", "--query", "label:bug is:open", "--board", "5f2a1b"}, Description: "Open bugs on one board"},
			},
		}, func(ctx context.Context, call *registry.Call, in cardsSearchInput) (registry.ListSource, error) {
			query := url.Values{
				"query":       {in.Query},
				"modelTypes":  {"cards"},
				"partial":     {"true"},
				"cards_limit": {strconv.Itoa(MaxSearchResults)},
			}
			if in.Board != "" {
				query.Set("idBoards", in.Board)
			}
			return &trello.LocalList{Sort: call.Sort, Load: func(ctx context.Context) ([]shape.Value, error) {
				body, err := call.Upstream.Do(ctx, &upstream.Request{Method: "GET", Path: "/search", Query: query})
				if err != nil {
					return nil, err
				}
				cards, ok := body.Get("cards")
				if !ok || cards.Kind != shape.Array {
					return nil, errs.New(errs.Upstream, "Trello returned an unexpected search response.")
				}
				return cards.Items, nil
			}}, nil
		}),

		registry.List[actionsListInput, trello.Action](registry.Spec{
			Name:        "actions.list",
			Description: "List activity on a board within a time window, newest first.",
			Collectable: true,
			TimeWindow:  true,
			Fields:      &registry.FieldSet{Default: trello.ActionDefault, Available: trello.ActionAvailable},
			Sort:        "date desc, id asc",
			Limits:      &registry.Limits{Default: 25, Max: 1000},
			Errors:      listErrors,
			Examples: []registry.Example{
				{Argv: []string{"actions", "list", "--board", "5f2a1b"}, Description: "Activity in the last 24 hours"},
				{Argv: []string{"actions", "list", "--board", "5f2a1b", "--type", "commentCard", "--since", "-7d"}, Description: "Comments in the last week"},
			},
		}, func(ctx context.Context, call *registry.Call, in actionsListInput) (registry.ListSource, error) {
			return &trello.ActionsSource{Upstream: call.Upstream, Board: in.Board, Filter: in.Type, Window: call.Window,
				Sort: call.Sort, PageSize: ActionsPageSize}, nil
		}),
	}
}

func filterByField(load func(context.Context) ([]shape.Value, error), field, want string) func(context.Context) ([]shape.Value, error) {
	return func(ctx context.Context) ([]shape.Value, error) {
		records, err := load(ctx)
		if err != nil {
			return nil, err
		}
		kept := records[:0]
		for _, record := range records {
			if value, ok := record.Get(field); ok && value.Text() == want {
				kept = append(kept, record)
			}
		}
		return kept, nil
	}
}
