package trello

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
	"github.com/guyravid/ai/cli/tools/trello/internal/upstream"
)

// MaxActionsPage is the largest page /boards/{id}/actions accepts.
const MaxActionsPage = 1000

// LocalList serves an endpoint that returns everything at once. It fetches once per invocation,
// sorts the whole set by the declared order, and pages locally by offset.
type LocalList struct {
	Load   func(ctx context.Context) ([]shape.Value, error)
	Sort   []shape.SortKey
	all    []shape.Value
	loaded bool
}

func (l *LocalList) MaxPageSize() int { return 1 << 30 }

func (l *LocalList) Fetch(ctx context.Context, continuation string, want int) (registry.Batch, error) {
	offset, err := parseOffset(continuation)
	if err != nil {
		return registry.Batch{}, err
	}
	if !l.loaded {
		records, err := l.Load(ctx)
		if err != nil {
			return registry.Batch{}, err
		}
		shape.SortRecords(records, l.Sort)
		l.all, l.loaded = records, true
	}
	total := int64(len(l.all))
	if offset > len(l.all) {
		offset = len(l.all)
	}
	end := min(offset+want, len(l.all))
	batch := registry.Batch{Records: l.all[offset:end], Total: &total}
	if end < len(l.all) {
		batch.Next = strconv.Itoa(end)
	}
	return batch, nil
}

func (l *LocalList) Resume(start string, delivered []shape.Value) string {
	offset, _ := parseOffset(start)
	return strconv.Itoa(offset + len(delivered))
}

func parseOffset(continuation string) (int, error) {
	if continuation == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(continuation)
	if err != nil || offset < 0 {
		return 0, errs.Usagef("The cursor is malformed.").
			WithHint("Pass meta.page.next_cursor back unchanged, or re-run the query without --cursor.")
	}
	return offset, nil
}

// ActionsSource pages /boards/{id}/actions backwards in time with before=<id>, the id-based style
// Trello uses for actions. The first page is bounded by the window's end.
type ActionsSource struct {
	Upstream registry.Doer
	Board    string
	Filter   string
	Window   *registry.Window
	Sort     []shape.SortKey
	PageSize int // largest page to request; 0 means MaxActionsPage
}

func (a *ActionsSource) MaxPageSize() int {
	if a.PageSize > 0 {
		return a.PageSize
	}
	return MaxActionsPage
}

func (a *ActionsSource) Fetch(ctx context.Context, continuation string, want int) (registry.Batch, error) {
	size := min(max(want, 1), a.MaxPageSize())
	query := url.Values{}
	query.Set("limit", strconv.Itoa(size))
	if a.Filter != "" {
		query.Set("filter", a.Filter)
	}
	if a.Window != nil {
		query.Set("since", a.Window.Since.UTC().Format(time.RFC3339))
	}
	if before, ok := strings.CutPrefix(continuation, "before:"); ok {
		query.Set("before", before)
	} else if continuation != "" {
		return registry.Batch{}, errs.Usagef("The cursor is malformed.")
	} else if a.Window != nil {
		query.Set("before", a.Window.Until.UTC().Format(time.RFC3339))
	}
	body, err := a.Upstream.Do(ctx, &upstream.Request{Method: "GET", Path: "/boards/" + url.PathEscape(a.Board) + "/actions", Query: query})
	if err != nil {
		return registry.Batch{}, err
	}
	if body.Kind != shape.Array {
		return registry.Batch{}, unexpected("a list of actions")
	}
	records := body.Items
	// Upstream orders by date, newest first. Re-sort within the page for a total order on ties.
	shape.SortRecords(records, a.Sort)
	batch := registry.Batch{Records: records}
	if len(records) == size {
		batch.Next = "before:" + recordID(records[len(records)-1])
	}
	return batch, nil
}

func (a *ActionsSource) Resume(start string, delivered []shape.Value) string {
	if len(delivered) == 0 {
		return start
	}
	return "before:" + recordID(delivered[len(delivered)-1])
}

func recordID(record shape.Value) string {
	id, _ := record.Get("id")
	return id.Text()
}

func unexpected(what string) error {
	return errs.New(errs.Upstream, "Trello returned an unexpected response; expected %s.", what)
}

// ArrayLoader returns a Load function for a GET endpoint that answers with a JSON array.
func ArrayLoader(doer registry.Doer, path string, query url.Values, what string) func(context.Context) ([]shape.Value, error) {
	return func(ctx context.Context) ([]shape.Value, error) {
		body, err := doer.Do(ctx, &upstream.Request{Method: "GET", Path: path, Query: query})
		if err != nil {
			return nil, err
		}
		if body.Kind != shape.Array {
			return nil, unexpected(fmt.Sprintf("a list of %s", what))
		}
		return body.Items, nil
	}
}
