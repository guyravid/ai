package telegram

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
	"github.com/guyravid/ai/cli/tools/telegram/internal/upstream"
)

const (
	// MaxLongPoll is the longest single getUpdates wait this tool asks for.
	MaxLongPoll = 25 * time.Second
	// longPollSlack is how long a long poll's HTTP deadline outlasts the poll itself.
	longPollSlack = 10 * time.Second
)

// WaitSource serves updates.wait: the "send, then wait for a person's answer" loop (plan, M6). It
// polls getUpdates without an offset, so nothing is confirmed (trap 2), and filters locally.
type WaitSource struct {
	Upstream registry.Doer
	Scope    ChatScope
	// AfterMessage and ReplyTo are the match filters; 0 means not given.
	AfterMessage int64
	ReplyTo      int64
	MaxWait      time.Duration
	Poll         time.Duration
	Now          func() time.Time
	Sleep        func(ctx context.Context, wait time.Duration) error
	// AckAvailable and AnyChatAvailable shape the hints in warnings.
	AckAvailable     bool
	AnyChatAvailable bool
}

func (w *WaitSource) MaxPageSize() int { return QueueWindow }

func (w *WaitSource) Resume(start string, delivered []shape.Value) string {
	if len(delivered) == 0 {
		return start
	}
	return strconv.FormatInt(lastID(delivered), 10)
}

// Fetch waits for a match and returns every match from the poll that found one. A continuation is a
// cursor from an earlier, paged answer: the queue is still there, so it reads the rest without
// waiting.
func (w *WaitSource) Fetch(ctx context.Context, continuation string, want int) (registry.Batch, error) {
	var after int64
	resuming := continuation != ""
	if resuming {
		parsed, err := strconv.ParseInt(continuation, 10, 64)
		if err != nil || parsed < 0 {
			return registry.Batch{}, errs.Usagef("The cursor is malformed.").
				WithHint("Pass meta.page.next_cursor back unchanged, or re-run the query without --cursor.")
		}
		after = parsed
	}
	started := w.Now()
	deadline := started.Add(w.MaxWait)
	snapshot := int64(-1) // the newest update_id when the wait began; -1 until the first poll
	timeout := time.Duration(0)

	for {
		queue, err := w.poll(ctx, timeout)
		if err != nil {
			return registry.Batch{}, err
		}
		full := len(queue) >= QueueWindow
		var parsedAll []Parsed
		var dropped DroppedChats
		var newest int64
		for _, item := range queue {
			parsed, ok := Flatten(item)
			if !ok {
				continue
			}
			newest = max(newest, parsed.ID)
			parsedAll = append(parsedAll, parsed)
		}
		if snapshot < 0 {
			snapshot = newest
		}
		var matches []Parsed
		for _, parsed := range parsedAll {
			if !w.Scope.Match(parsed) {
				if w.Scope.Allowlisting() {
					dropped.add(parsed)
				}
				continue
			}
			if resuming && parsed.ID <= after || !w.matches(parsed, snapshot) {
				continue
			}
			matches = append(matches, parsed)
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })

		notices := allowlistNotices(w.Scope, dropped, w.AnyChatAvailable)
		if full {
			notices = append(notices, queueFullNotice(w.AckAvailable))
		}
		if len(matches) > 0 || resuming {
			return w.batch(matches, notices, want, !full), nil
		}
		if full {
			// 100 are pending and none match: newer updates are invisible, so waiting would be blind.
			return w.batch(nil, notices, want, false), nil
		}
		if finished, err := w.finished(ctx, deadline); err != nil {
			return registry.Batch{}, err
		} else if finished {
			return w.noReply(notices, want), nil
		}

		if len(queue) == 0 {
			// Nothing pending: long poll, which returns the moment something arrives.
			seconds := w.longPollSeconds(ctx, deadline)
			if seconds < 1 {
				return w.noReply(notices, want), nil
			}
			timeout = time.Duration(seconds) * time.Second
			continue
		}
		// Updates are pending, so a long poll would return at once. Sleep instead, never busy-loop.
		pause := min(w.Poll, max(deadline.Sub(w.Now()), 0))
		if sleepErr := w.Sleep(ctx, pause); sleepErr != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return w.noReply(notices, want), nil
			}
			return registry.Batch{}, errs.New(errs.Canceled, "Interrupted.")
		}
		timeout = 0
	}
}

// matches applies the wait's own filters. Neither filter given means any message newer than the
// queue was when the wait began.
func (w *WaitSource) matches(parsed Parsed, snapshot int64) bool {
	if kind, _ := parsed.Record.Get("type"); kind.Text() != "message" && kind.Text() != "channel_post" {
		return false
	}
	if w.AfterMessage == 0 && w.ReplyTo == 0 {
		return parsed.ID > snapshot
	}
	if w.AfterMessage != 0 {
		if id, ok := intMember(parsed.Record, "message_id"); !ok || id <= w.AfterMessage {
			return false
		}
	}
	if w.ReplyTo != 0 {
		if id, ok := intMember(parsed.Record, "reply_to_message_id"); !ok || id != w.ReplyTo {
			return false
		}
	}
	return true
}

func (w *WaitSource) poll(ctx context.Context, timeout time.Duration) ([]shape.Value, error) {
	body := map[string]any{"limit": QueueWindow}
	request := &upstream.Request{Method: "POST", Path: "/getUpdates", Body: body}
	if timeout > 0 {
		body["timeout"] = int64(timeout / time.Second)
		request.Timeout = timeout + longPollSlack
	}
	value, err := w.Upstream.Do(ctx, request)
	if err != nil {
		return nil, err
	}
	if value.Kind != shape.Array {
		return nil, errs.New(errs.Upstream, "Telegram returned an unexpected response; expected a list of updates.")
	}
	return value.Items, nil
}

// finished reports whether the wait is over: max-wait elapsed, or the invocation's budget did. A
// cancellation is an error.
func (w *WaitSource) finished(ctx context.Context, deadline time.Time) (bool, error) {
	switch ctx.Err() {
	case nil:
	case context.DeadlineExceeded:
		return true, nil
	default:
		return false, errs.New(errs.Canceled, "Interrupted.")
	}
	return !w.Now().Before(deadline), nil
}

// longPollSeconds is how long the next long poll may wait: up to 25 s, and never past max-wait or
// the invocation's budget. Telegram takes whole seconds.
func (w *WaitSource) longPollSeconds(ctx context.Context, deadline time.Time) int {
	remaining := deadline.Sub(w.Now())
	if limit, ok := ctx.Deadline(); ok {
		remaining = min(remaining, time.Until(limit))
	}
	return int(min(remaining, MaxLongPoll) / time.Second)
}

func (w *WaitSource) batch(matches []Parsed, notices []registry.Notice, want int, exactTotal bool) registry.Batch {
	batch := registry.Batch{Notices: notices}
	records := make([]shape.Value, len(matches))
	for index, parsed := range matches {
		records[index] = parsed.Record
	}
	if exactTotal {
		total := int64(len(records))
		batch.Total = &total
	}
	if len(records) > want {
		batch.Records = records[:want]
		batch.Next = strconv.FormatInt(lastID(batch.Records), 10)
	} else {
		batch.Records = records
	}
	return batch
}

func queueFullNotice(ackAvailable bool) registry.Notice {
	return (&UpdatesSource{AckAvailable: ackAvailable}).queueFullNotice()
}

// noReply is an empty, successful answer. The warning tells it apart from "nothing to read".
func (w *WaitSource) noReply(notices []registry.Notice, want int) registry.Batch {
	return w.batch(nil, append(notices, registry.Notice{Code: "no_reply",
		Message: fmt.Sprintf("No matching message within %s.", w.MaxWait)}), want, true)
}
