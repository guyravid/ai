package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/trello/internal/config"
	"github.com/guyravid/ai/cli/tools/trello/internal/datasets"
	"github.com/guyravid/ai/cli/tools/trello/internal/envelope"
	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
	"github.com/guyravid/ai/cli/tools/trello/internal/registry"
	"github.com/guyravid/ai/cli/tools/trello/internal/shape"
)

// typedParam restores a parameter that a cursor stored as text to the type the command declares,
// so a non-string parameter survives a round trip through --cursor.
func typedParam(command *registry.Command, name, text string) any {
	param := command.Param(name)
	if param == nil {
		return text
	}
	switch param.Type {
	case registry.TypeInteger:
		if number, err := strconv.ParseInt(text, 10, 64); err == nil {
			return number
		}
	case registry.TypeBoolean:
		if flag, err := strconv.ParseBool(text); err == nil {
			return flag
		}
	}
	return text
}

// previewStringChars bounds each value in a refusal's preview so error.details stays small.
const previewStringChars = 200

// domain runs a Trello command: object, list, or write.
func (s *session) domain(ctx context.Context) *Response {
	command := s.invocation.Command
	if err := s.newClient(); err != nil {
		return s.fail(err)
	}
	budget := s.settings.Duration("BUDGET")
	if s.invocation.Bool("all") && s.settings.Get("BUDGET").Origin == config.OriginBuiltin {
		budget = allBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	switch command.Kind {
	case registry.KindObject:
		return s.object(ctx)
	case registry.KindList:
		return s.list(ctx)
	case registry.KindWrite:
		return s.write(ctx)
	}
	return s.fail(errs.New(errs.Internal, "Command %s has no handler.", command.Name))
}

func (s *session) call(window *registry.Window) *registry.Call {
	return &registry.Call{Upstream: s.doer(), Window: window, Sort: shape.ParseSort(s.invocation.Command.Sort)}
}

func (s *session) object(ctx context.Context) *Response {
	command := s.invocation.Command
	fields, err := shape.ResolveFields(s.invocation.Flags["fields"], command.Fields.Default, command.Fields.Available)
	if err != nil {
		return s.fail(err)
	}
	input, err := command.Decode(s.invocation.Params)
	if err != nil {
		return s.fail(err)
	}
	value, runErr := command.RunObject(ctx, s.call(nil), input)
	if runErr != nil {
		if response, ok := s.asDryRun(runErr); ok {
			return response
		}
		return s.fail(s.upstreamError(ctx, runErr))
	}
	return s.respondObject(value, fields)
}

// listRequest is a list call with everything resolved, from flags or from a cursor.
type listRequest struct {
	params     map[string]any
	fields     []string
	fieldsExpr string
	limit      int
	window     *registry.Window
	windowFrom string
	start      string // upstream continuation this call starts from
}

func (s *session) resolveList() (*listRequest, *errs.Error) {
	command := s.invocation.Command
	request := &listRequest{params: s.invocation.Params, fieldsExpr: s.invocation.Flags["fields"]}
	limitFromCursor := ""
	if text, ok := s.invocation.Flags["cursor"]; ok {
		if len(s.invocation.Params) > 0 || s.invocation.Has("fields") || s.invocation.Has("since") ||
			s.invocation.Has("until") || s.invocation.Has("all") {
			return nil, errs.Usagef("--cursor continues the original query; pass it without parameters, --fields, --since, --until, or --all.").
				WithHint(fmt.Sprintf("%s %s --cursor <next_cursor>", s.app.Build.Tool, strings.Join(command.Argv(), " ")))
		}
		cursor, err := envelope.DecodeCursor(text, command.Name)
		if err != nil {
			return nil, err
		}
		request.params = map[string]any{}
		for key, value := range cursor.Q {
			switch key {
			case "fields":
				request.fieldsExpr = value
			case "limit":
				limitFromCursor = value
			default:
				request.params[key] = typedParam(command, key, value)
			}
		}
		request.start = cursor.U
		if cursor.W != nil {
			since, sinceErr := time.Parse(time.RFC3339, cursor.W.Since)
			until, untilErr := time.Parse(time.RFC3339, cursor.W.Until)
			if sinceErr != nil || untilErr != nil {
				return nil, errs.Usagef("The cursor is malformed.")
			}
			request.window, request.windowFrom = &registry.Window{Since: since, Until: until}, "cursor"
		}
	}
	fields, err := shape.ResolveFields(request.fieldsExpr, command.Fields.Default, command.Fields.Available)
	if err != nil {
		return nil, err
	}
	request.fields = fields
	request.limit = s.resolveLimit(command.Limits, limitFromCursor)
	if command.TimeWindow && request.window == nil {
		window, source, windowErr := resolveWindow(s.invocation, s.now)
		if windowErr != nil {
			return nil, windowErr
		}
		request.window, request.windowFrom = window, source
	}
	return request, nil
}

func (s *session) setWindow(request *listRequest) {
	if request.window != nil {
		s.envelope.Meta.Window = &envelope.Window{
			Since: formatInstant(request.window.Since), Until: formatInstant(request.window.Until), Source: request.windowFrom,
		}
	}
}

// cursorParams records the resolved call so a cursor alone can continue it (contract §9.3).
func (s *session) cursorParams(request *listRequest) map[string]string {
	params := map[string]string{"fields": strings.Join(request.fields, ","), "limit": strconv.Itoa(request.limit)}
	for key, value := range request.params {
		params[key] = fmt.Sprint(value)
	}
	return params
}

func (s *session) cursorWindow(request *listRequest) *envelope.Window {
	if request.window == nil {
		return nil
	}
	return &envelope.Window{Since: formatInstant(request.window.Since), Until: formatInstant(request.window.Until)}
}

func (s *session) list(ctx context.Context) *Response {
	command := s.invocation.Command
	request, err := s.resolveList()
	if err != nil {
		return s.fail(err)
	}
	input, err := command.Decode(request.params)
	if err != nil {
		return s.fail(err)
	}
	s.setWindow(request)
	s.envelope.Meta.Sort = command.Sort
	source, runErr := command.RunList(ctx, s.call(request.window), input)
	if runErr != nil {
		return s.fail(s.upstreamError(ctx, runErr))
	}
	if s.invocation.Bool("all") {
		return s.collect(ctx, source, request)
	}

	maxPages := int(s.settings.Int("MAX_PAGES"))
	records, next, total, truncated, fetchErr := fill(ctx, source, request.start, request.limit, maxPages)
	if fetchErr != nil {
		if response, ok := s.asDryRun(fetchErr); ok {
			return response
		}
		if len(records) == 0 {
			return s.fail(s.upstreamError(ctx, fetchErr))
		}
	}
	page := envelope.Page{Limit: request.limit, Total: total, TotalIsExact: total != nil}
	cursorFor := func(delivered []shape.Value) string {
		return envelope.EncodeCursor(envelope.Cursor{C: command.Name, U: source.Resume(request.start, delivered),
			Q: s.cursorParams(request), W: s.cursorWindow(request)})
	}
	if next != "" || truncated != "" || fetchErr != nil {
		cursor := cursorFor(records)
		page.HasMore, page.NextCursor = true, &cursor
	}
	if truncated != "" {
		page.Truncated, page.TruncatedReason = true, truncated
	}
	response := s.respondList(records, request.fields, page, func(index int) string { return cursorFor(records[:index]) })
	if fetchErr != nil {
		return s.partial(response, fetchErr, len(records))
	}
	return response
}

// fill pages through upstream until the limit is met or a cap stops it (contract §9.1). Pages are
// fetched one after another, never in parallel.
func fill(ctx context.Context, source registry.ListSource, start string, limit, maxPages int) ([]shape.Value, string, *int64, string, error) {
	var records []shape.Value
	var total *int64
	continuation := start
	for pages := 0; len(records) < limit; pages++ {
		if pages == maxPages {
			return records, continuation, total, "max_pages", nil
		}
		if ctx.Err() != nil && len(records) > 0 {
			return records, continuation, total, "budget", nil
		}
		batch, err := source.Fetch(ctx, continuation, limit-len(records))
		if err != nil {
			if len(records) > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return records, continuation, total, "budget", nil
			}
			return records, continuation, total, "", err
		}
		records = append(records, batch.Records...)
		if batch.Total != nil {
			total = batch.Total
		}
		continuation = batch.Next
		if continuation == "" {
			break
		}
	}
	if len(records) > limit {
		records = records[:limit]
	}
	return records, continuation, total, "", nil
}

// partial converts a finished list response into a partial failure that keeps its data.
func (s *session) partial(response *Response, cause error, retrieved int) *Response {
	mapped := errs.From(cause)
	data := s.envelope.Data
	s.envelope.Meta.Errors = append(s.envelope.Meta.Errors, map[string]any{"code": string(mapped.Code), "message": mapped.Message})
	s.envelope.SetError(errs.New(errs.Partial, "Retrieved %d records before Trello failed: %s", retrieved, mapped.Message).
		WithHint(commandLine(s.app.Build.Tool, s.invocation)))
	s.envelope.Data = data
	return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
}

// upstreamError maps any handler failure onto the contract's codes.
func (s *session) upstreamError(ctx context.Context, err error) *errs.Error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		var typed *errs.Error
		if !errors.As(err, &typed) || typed.Code != errs.Timeout {
			return errs.New(errs.Timeout, "The invocation's time budget ran out.").
				WithHint("Raise --budget, or narrow the query.")
		}
	}
	return errs.From(err)
}

// collect fetches the whole result set in the largest pages upstream allows, stores it, and
// returns the first part (contract §10.1).
func (s *session) collect(ctx context.Context, source registry.ListSource, request *listRequest) *Response {
	command := s.invocation.Command
	store, err := s.openStore()
	if err != nil {
		return s.fail(err)
	}
	params := map[string]string{}
	for key, value := range request.params {
		name := key
		if param := command.Param(key); param != nil && param.Positional {
			name = "<" + key
		}
		params[name] = fmt.Sprint(value)
	}
	var window *datasets.Window
	if request.window != nil {
		window = &datasets.Window{Since: formatInstant(request.window.Since), Until: formatInstant(request.window.Until)}
	}
	id := datasets.NewID(s.invocation.Bool("deterministic"), requestKey(s.app.Build.Tool, command.Name, params, window, request.fields))
	writer, err := store.Create(datasets.Sidecar{
		ID: id, Tool: s.app.Build.Tool, Command: command.Name, Params: params, Fields: request.fields,
		Sort: command.Sort, Window: window, ContractVersion: envelope.ContractVersion, ToolVersion: s.app.Build.Version,
	}, s.redactor.Apply)
	if err != nil {
		return s.fail(err)
	}
	defer writer.Abort()

	maxPages := int(s.settings.Int("MAX_PAGES"))
	if s.settings.Get("MAX_PAGES").Origin == config.OriginBuiltin {
		maxPages = allMaxPages
	}
	maxRecords := s.settings.Int("DATASET_MAX_RECORDS")
	maxBytes := s.settings.Size("DATASET_MAX_BYTES")
	reason := ""
	var fetchErr error
	continuation := ""
collecting:
	for pages := 0; ; pages++ {
		if pages == maxPages {
			reason = "max_pages"
			break
		}
		batch, err := source.Fetch(ctx, continuation, source.MaxPageSize())
		if err != nil {
			if response, ok := s.asDryRun(err); ok {
				return response
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && writer.Records() > 0 {
				reason = "budget"
			} else {
				fetchErr, reason = s.upstreamError(ctx, err), "upstream_error"
			}
			break
		}
		for _, record := range batch.Records {
			if writer.Records() >= maxRecords {
				reason = "max_records"
				break collecting
			}
			if writer.Bytes() >= maxBytes {
				reason = "max_bytes"
				break collecting
			}
			if err := writer.Append(s.projectAndStrip(record, request.fields)); err != nil {
				return s.fail(errs.Configf("Writing the dataset failed: %s.", err.Error()))
			}
		}
		continuation = batch.Next
		if continuation == "" {
			break
		}
	}
	if fetchErr != nil && writer.Records() == 0 {
		return s.fail(errs.From(fetchErr))
	}
	sidecar, commitErr := writer.Commit(reason == "", reason)
	if commitErr != nil {
		return s.fail(commitErr)
	}
	if !sidecar.Complete {
		s.envelope.AddWarning("dataset_incomplete", fmt.Sprintf("Collection stopped early (%s); the dataset holds %d records.", reason, sidecar.RecordCount))
	}
	response := s.readDataset(store, sidecar, 0, request.limit, request.fields, true)
	if fetchErr != nil {
		return s.partial(response, fetchErr, int(sidecar.RecordCount))
	}
	return response
}

func requestKey(tool, command string, params map[string]string, window *datasets.Window, fields []string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString(tool + "\x00" + command)
	for _, key := range keys {
		builder.WriteString("\x00" + key + "=" + params[key])
	}
	if window != nil {
		builder.WriteString("\x00" + window.Since + "\x00" + window.Until)
	}
	builder.WriteString("\x00" + strings.Join(fields, ","))
	return builder.String()
}

func (s *session) write(ctx context.Context) *Response {
	command := s.invocation.Command
	input, err := command.Decode(s.invocation.Params)
	if err != nil {
		return s.fail(err)
	}
	request, buildErr := command.BuildWrite(ctx, s.call(nil), input)
	if buildErr != nil {
		return s.fail(errs.From(buildErr))
	}
	preview := s.client.Preview(request)
	if s.invocation.Bool("dry-run") {
		s.envelope.Data = shape.NewObject(shape.Field{Key: "preview", Value: preview})
		return s.finish(Encode(s.envelope, s.invocation.Bool("pretty")))
	}
	if !s.invocation.Bool("confirm") {
		// error.details is capped at 2048 bytes, so long body values are shortened here;
		// --dry-run shows the preview in full.
		shortened, elided := shape.Cap(preview, previewStringChars, 8, "")
		refusal := errs.New(errs.Refused, "Writes require --confirm.").
			WithHint(commandLine(s.app.Build.Tool, s.invocation, "--confirm")).
			WithDetail("reason", "confirmation_required").
			WithDetail("preview", shortened)
		if len(elided) > 0 {
			refusal.WithDetail("preview_elided_fields", elided)
		}
		return s.fail(refusal)
	}
	value, sendErr := s.client.Do(ctx, request)
	if sendErr != nil {
		return s.fail(s.upstreamError(ctx, sendErr))
	}
	return s.respondObject(value, nil)
}
