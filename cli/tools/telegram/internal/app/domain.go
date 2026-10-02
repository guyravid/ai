package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
	"github.com/guyravid/ai/cli/tools/telegram/internal/envelope"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
)

// previewStringChars bounds each value in a refusal's preview so error.details stays small.
const previewStringChars = 200

// domain runs a Telegram command: object, list, or write.
func (s *session) domain(ctx context.Context) *Response {
	command := s.invocation.Command
	// A profile that would use the default bot's token fails here, before anything is built or sent,
	// so --dry-run and a refused write fail the same way a real call does.
	if s.tokenErr != nil {
		return s.fail(s.tokenErr)
	}
	if err := s.newClient(); err != nil {
		return s.fail(err)
	}
	budget, budgetErr := s.budget()
	if budgetErr != nil {
		return s.fail(budgetErr)
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

// budget is the deadline for the whole invocation: the BUDGET setting, unless the command waits and
// declares its own default (updates.wait: --max-wait plus 30 s). A cursor continues a finished wait,
// which reads without waiting, so it keeps the setting.
func (s *session) budget() (time.Duration, *errs.Error) {
	command := s.invocation.Command
	configured := s.settings.Duration("BUDGET")
	if command.Budget == nil || s.invocation.Has("cursor") {
		return configured, nil
	}
	input, err := command.Decode(s.invocation.Params)
	if err != nil {
		return 0, err
	}
	explicit := s.settings.Get("BUDGET").Origin != config.OriginBuiltin
	budget, budgetErr := command.Budget(input, configured, explicit)
	if budgetErr != nil {
		return 0, errs.From(budgetErr)
	}
	return budget, nil
}

func (s *session) call(window *registry.Window) *registry.Call {
	return &registry.Call{Upstream: s.doer(), Window: window, Sort: shape.ParseSort(s.invocation.Command.Sort),
		DefaultChat: s.settings.String("DEFAULT_CHAT"), AllowedChats: config.SplitChats(s.settings.String("ALLOWED_CHATS")),
		ParseMode: s.settings.String("PARSE_MODE"), PollInterval: s.settings.Duration("POLL_INTERVAL"),
		Now: s.app.Now, Sleep: s.sleep,
		CommandLine: func(extra ...string) string { return commandLine(s.app.Build.Tool, s.invocation, extra...) }}
}

// sleep waits between polls. Tests inject App.Sleep; otherwise it waits for real, ending early when
// the call is canceled.
func (s *session) sleep(ctx context.Context, wait time.Duration) error {
	if s.app.Sleep != nil {
		return s.app.Sleep(ctx, wait)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
			s.invocation.Has("until") {
			return nil, errs.Usagef("--cursor continues the original query; pass it without parameters, --fields, --since, or --until.").
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

	maxPages := int(s.settings.Int("MAX_PAGES"))
	records, next, total, truncated, notices, fetchErr := fill(ctx, source, request.start, request.limit, maxPages)
	for _, notice := range notices {
		s.envelope.AddWarning(notice.Code, notice.Message)
	}
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
func fill(ctx context.Context, source registry.ListSource, start string, limit, maxPages int) ([]shape.Value, string, *int64, string, []registry.Notice, error) {
	var records []shape.Value
	var notices []registry.Notice
	var total *int64
	continuation := start
	for pages := 0; len(records) < limit; pages++ {
		if pages == maxPages {
			return records, continuation, total, "max_pages", notices, nil
		}
		if ctx.Err() != nil && len(records) > 0 {
			return records, continuation, total, "budget", notices, nil
		}
		batch, err := source.Fetch(ctx, continuation, limit-len(records))
		if err != nil {
			if len(records) > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return records, continuation, total, "budget", notices, nil
			}
			return records, continuation, total, "", notices, err
		}
		records = append(records, batch.Records...)
		notices = append(notices, batch.Notices...)
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
	return records, continuation, total, "", notices, nil
}

// typedParam restores a parameter that a cursor stored as text to the type the command declares,
// so an integer such as --after survives a round trip through --cursor.
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

// partial converts a finished list response into a partial failure that keeps its data.
func (s *session) partial(response *Response, cause error, retrieved int) *Response {
	mapped := errs.From(cause)
	data := s.envelope.Data
	s.envelope.Meta.Errors = append(s.envelope.Meta.Errors, map[string]any{"code": string(mapped.Code), "message": mapped.Message})
	s.envelope.SetError(errs.New(errs.Partial, "Retrieved %d records before Telegram failed: %s", retrieved, mapped.Message).
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
	if command.Result != nil {
		reshaped, resultErr := command.Result(input, value)
		if resultErr != nil {
			return s.fail(errs.From(resultErr))
		}
		value = reshaped
	}
	return s.respondObject(value, nil)
}
