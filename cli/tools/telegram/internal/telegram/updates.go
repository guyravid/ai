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

// QueueWindow is how many unconfirmed updates getUpdates shows without an offset, and so the most
// this tool can ever see at once (plan, trap 4).
const QueueWindow = 100

// Parsed is a flattened update, with the few values filtering needs kept beside the record.
type Parsed struct {
	ID           int64
	Date         time.Time // zero when the update carries no usable date
	ChatID       string    // the number as Telegram wrote it
	ChatUsername string
	Record       shape.Value
}

// Flatten turns one raw update into the flat record --fields works on (plan, "Update record").
// Telegram's update is {update_id, <one payload key>}; the payload is a message for message,
// edited_message, channel_post and edited_channel_post, a CallbackQuery for callback_query, and
// something else again (for example a ChatMemberUpdated for my_chat_member). It reports false for
// anything without an update_id.
//
// TODO(M9): callback_query takes chat, message_id and date from its nested message, and the other
// payload types take chat, from and date from the payload itself, as ChatMemberUpdated documents.
// Confirm both against live updates.
func Flatten(raw shape.Value) (Parsed, bool) {
	idValue, ok := raw.Get("update_id")
	if !ok {
		return Parsed{}, false
	}
	id, err := strconv.ParseInt(string(idValue.Raw), 10, 64)
	if err != nil {
		return Parsed{}, false
	}
	parsed := Parsed{ID: id}
	kind, payload := "", shape.Value{}
	for _, field := range raw.Fields {
		if field.Key != "update_id" && field.Value.Kind == shape.Object {
			kind, payload = field.Key, field.Value
			break
		}
	}
	record := []shape.Field{{Key: "update_id", Value: shape.Int(id)}}
	if kind == "" {
		record = append(record, shape.Field{Key: "type", Value: shape.String("unknown")})
		parsed.Record = shape.NewObject(record...)
		return parsed, true
	}
	record = append(record, shape.Field{Key: "type", Value: shape.String(kind)})

	container, from := payload, shape.Value{}
	if kind == "callback_query" {
		from, _ = payload.Get("from")
		container, _ = payload.Get("message")
	} else {
		from, _ = payload.Get("from")
	}

	if date, ok := unixDate(container); ok {
		parsed.Date = date
		record = append(record, shape.Field{Key: "date", Value: shape.String(date.Format(time.RFC3339))})
	}
	if chat, ok := container.Get("chat"); ok && chat.Kind == shape.Object {
		fields, chatID, username := pick(chat, "id", "type", "title", "username")
		parsed.ChatID, parsed.ChatUsername = chatID, username
		if len(fields) > 0 {
			record = append(record, shape.Field{Key: "chat", Value: shape.NewObject(fields...)})
		}
	}
	if from.Kind == shape.Object {
		if fields, _, _ := pick(from, "id", "username", "first_name"); len(fields) > 0 {
			record = append(record, shape.Field{Key: "from", Value: shape.NewObject(fields...)})
		}
	}
	for _, key := range []string{"message_id", "text", "caption"} {
		if value, ok := container.Get(key); ok && !value.IsNull() {
			record = append(record, shape.Field{Key: key, Value: value})
		}
	}
	if reply, ok := container.Get("reply_to_message"); ok {
		if value, ok := reply.Get("message_id"); ok && !value.IsNull() {
			record = append(record, shape.Field{Key: "reply_to_message_id", Value: value})
		}
	}
	if document, ok := container.Get("document"); ok {
		if name, ok := document.Get("file_name"); ok && !name.IsNull() {
			record = append(record, shape.Field{Key: "document", Value: shape.NewObject(shape.Field{Key: "file_name", Value: name})})
		}
	}
	if fileID := largestPhoto(container); fileID != "" {
		record = append(record, shape.Field{Key: "photo", Value: shape.String(fileID)})
	}
	parsed.Record = shape.NewObject(record...)
	return parsed, true
}

// unixDate reads a Unix-seconds date member. Zero means "no date" (an inaccessible message), so it
// is treated as absent.
func unixDate(container shape.Value) (time.Time, bool) {
	value, ok := container.Get("date")
	if !ok || !value.IsNumber() {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(string(value.Raw), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0).UTC(), true
}

// pick copies the named members of an object that are present and not null, in the given order. It
// also returns the id's text and the username, for filtering.
func pick(object shape.Value, keys ...string) ([]shape.Field, string, string) {
	var fields []shape.Field
	id, username := "", ""
	for _, key := range keys {
		value, ok := object.Get(key)
		if !ok || value.IsNull() {
			continue
		}
		fields = append(fields, shape.Field{Key: key, Value: value})
		switch key {
		case "id":
			id = string(value.Raw)
		case "username":
			username = value.Text()
		}
	}
	return fields, id, username
}

// largestPhoto returns the file_id of the largest PhotoSize of a message, by pixels, the later
// size winning a tie. TODO(M9): confirm that sizes arrive in ascending order.
func largestPhoto(container shape.Value) string {
	photo, ok := container.Get("photo")
	if !ok || photo.Kind != shape.Array {
		return ""
	}
	best, bestPixels := "", int64(-1)
	for _, size := range photo.Items {
		id, ok := size.Get("file_id")
		if !ok || !id.IsString() {
			continue
		}
		width, _ := intMember(size, "width")
		height, _ := intMember(size, "height")
		if pixels := width * height; pixels >= bestPixels {
			best, bestPixels = id.Text(), pixels
		}
	}
	return best
}

func intMember(object shape.Value, key string) (int64, bool) {
	value, ok := object.Get(key)
	if !ok || !value.IsNumber() {
		return 0, false
	}
	number, err := strconv.ParseInt(string(value.Raw), 10, 64)
	return number, err == nil
}

// UpdatesSource serves updates.list. getUpdates is called once, without offset or allowed_updates
// (plan, traps 2 and 3), and everything else is filtered and paged locally over what came back.
// The cursor is the last update_id delivered.
type UpdatesSource struct {
	Upstream registry.Doer
	// Chat keeps only updates from this chat: an id, or a public @username. "" keeps every chat,
	// unless FilterAllowed narrows them to Allowed (plan, "Allowlist rule").
	Chat          string
	Allowed       []string
	FilterAllowed bool
	After         int64
	Window        *registry.Window
	// AckAvailable says whether the tool can acknowledge updates, which the full-queue warning
	// then names as the way out.
	AckAvailable bool
	// AnyChatAvailable says whether the build offers --allow-any-chat, which the empty-allowlist
	// warning then names as a way out.
	AnyChatAvailable bool

	loaded  bool
	matched []Parsed
	full    bool
	dropped DroppedChats
}

func (u *UpdatesSource) MaxPageSize() int { return QueueWindow }

func (u *UpdatesSource) load(ctx context.Context) error {
	body, err := u.Upstream.Do(ctx, &upstream.Request{Method: "POST", Path: "/getUpdates",
		Body: map[string]any{"limit": QueueWindow}})
	if err != nil {
		return err
	}
	if body.Kind != shape.Array {
		return errs.New(errs.Upstream, "Telegram returned an unexpected response; expected a list of updates.")
	}
	u.full = len(body.Items) >= QueueWindow
	for _, item := range body.Items {
		parsed, ok := Flatten(item)
		if !ok || parsed.ID <= u.After {
			continue
		}
		if !u.scope().Match(parsed) {
			if u.scope().Allowlisting() {
				u.dropped.add(parsed)
			}
			continue
		}
		if !u.inWindow(parsed) {
			continue
		}
		u.matched = append(u.matched, parsed)
	}
	// The declared sort is update_id asc; update_id is unique, so the order is total.
	sort.Slice(u.matched, func(i, j int) bool { return u.matched[i].ID < u.matched[j].ID })
	u.loaded = true
	return nil
}

func (u *UpdatesSource) scope() ChatScope {
	return ChatScope{Chat: u.Chat, Allowed: u.Allowed, FilterAllowed: u.FilterAllowed}
}

// inWindow applies the time window to the update's date. An update with no date cannot be placed in
// a window, so it is kept rather than silently hidden.
func (u *UpdatesSource) inWindow(parsed Parsed) bool {
	if u.Window == nil || parsed.Date.IsZero() {
		return true
	}
	return !parsed.Date.Before(u.Window.Since) && !parsed.Date.After(u.Window.Until)
}

func (u *UpdatesSource) Fetch(ctx context.Context, continuation string, want int) (registry.Batch, error) {
	var after int64
	if continuation != "" {
		parsed, err := strconv.ParseInt(continuation, 10, 64)
		if err != nil || parsed < 0 {
			return registry.Batch{}, errs.Usagef("The cursor is malformed.").
				WithHint("Pass meta.page.next_cursor back unchanged, or re-run the query without --cursor.")
		}
		after = parsed
	}
	var batch registry.Batch
	if !u.loaded {
		if err := u.load(ctx); err != nil {
			return registry.Batch{}, err
		}
		batch.Notices = append(batch.Notices, allowlistNotices(u.scope(), u.dropped, u.AnyChatAvailable)...)
		if u.full {
			batch.Notices = append(batch.Notices, u.queueFullNotice())
		}
	}
	if !u.full {
		total := int64(len(u.matched))
		batch.Total = &total
	}
	var remaining []shape.Value
	for _, parsed := range u.matched {
		if parsed.ID > after {
			remaining = append(remaining, parsed.Record)
		}
	}
	if len(remaining) > want {
		batch.Records = remaining[:want]
		batch.Next = strconv.FormatInt(lastID(batch.Records), 10)
	} else {
		batch.Records = remaining
	}
	return batch, nil
}

func (u *UpdatesSource) Resume(start string, delivered []shape.Value) string {
	if len(delivered) == 0 {
		return start
	}
	return strconv.FormatInt(lastID(delivered), 10)
}

func lastID(records []shape.Value) int64 {
	id, _ := intMember(records[len(records)-1], "update_id")
	return id
}

func (u *UpdatesSource) queueFullNotice() registry.Notice {
	message := fmt.Sprintf("Telegram returned %d updates, the most it shows at once. Newer updates are invisible until earlier ones are acknowledged; unacknowledged updates are dropped after 24 hours.", QueueWindow)
	if u.AckAvailable {
		message += " Acknowledge with: " + toolName + " updates ack --through <update_id> --confirm"
	} else {
		message += " This build cannot acknowledge updates."
	}
	return registry.Notice{Code: "queue_window_full", Message: message}
}

// allowlistNotices are the warnings an allowlist filter owes the caller: how much it left out, and
// that an empty allowlist leaves out everything. Counts only, never chat ids.
func allowlistNotices(scope ChatScope, dropped DroppedChats, anyChatAvailable bool) []registry.Notice {
	if !scope.Allowlisting() {
		return nil
	}
	var notices []registry.Notice
	if len(scope.Allowed) == 0 {
		message := "This profile allows no chats, so every update was filtered out. Set default_chat or allowed_chats (see `" +
			toolName + " teach chats`)"
		if anyChatAvailable {
			message += ", or pass --allow-any-chat to see every chat"
		}
		notices = append(notices, registry.Notice{Code: "no_allowed_chats", Message: message + "."})
	}
	if dropped.Updates > 0 {
		notices = append(notices, registry.Notice{Code: "filtered_chats", Message: fmt.Sprintf(
			"%d update(s) from %d chat(s) outside this profile's allowed chats were left out.", dropped.Updates, dropped.Chats())})
	}
	return notices
}
