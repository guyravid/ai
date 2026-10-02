// Package commands defines the Telegram domain commands as registry entries.
package commands

import (
	"context"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
	"github.com/guyravid/ai/cli/tools/telegram/internal/telegram"
	"github.com/guyravid/ai/cli/tools/telegram/internal/upstream"
)

var (
	botErrors     = []errs.Code{errs.Usage, errs.Config, errs.Auth, errs.RateLimited, errs.Timeout, errs.Network, errs.Upstream}
	chatErrors    = append(append([]errs.Code(nil), botErrors...), errs.NotFound, errs.ChatUnreachable)
	updatesErrors = append(append([]errs.Code(nil), botErrors...), errs.Conflict)
)

// ackAvailable says whether the build can acknowledge updates, which the full-queue warning names
// as the way out. updates.ack is a write, so only the full build has it.
const ackAvailable = WritesEnabled

// anyChatAvailable says whether the build offers --allow-any-chat, which is a write-build feature
// (plan, "Allowlist rule").
const anyChatAvailable = WritesEnabled

type botGetInput struct{}

type chatsGetInput struct {
	Chat         string `json:"chat,omitempty" jsonschema:"Chat id or public @username; defaults to the profile's default_chat"`
	AllowAnyChat bool   `json:"allow_any_chat,omitempty" jsonschema:"Address a chat outside the profile's allowed chats"`
}

type updatesListInput struct {
	Chat         string `json:"chat,omitempty" jsonschema:"Only updates from this chat id or public @username; it must be an allowed chat"`
	After        int64  `json:"after,omitempty" jsonschema:"Only updates with an update_id greater than this"`
	AllowAnyChat bool   `json:"allow_any_chat,omitempty" jsonschema:"List updates from every chat, not only the profile's allowed chats"`
}

type updatesWaitInput struct {
	Chat         string `json:"chat,omitempty" jsonschema:"Chat to wait in; defaults to default_chat, else every allowed chat"`
	AfterMessage int64  `json:"after_message,omitempty" jsonschema:"Match a message with a message_id greater than this"`
	ReplyTo      int64  `json:"reply_to,omitempty" jsonschema:"Match a message that replies to this message_id"`
	MaxWait      string `json:"max_wait,omitempty" jsonschema:"How long to wait for a match: 60s by default, 1h at most. The default budget for this command is max_wait plus 30s"`
	AllowAnyChat bool   `json:"allow_any_chat,omitempty" jsonschema:"Wait in any chat, not only the profile's allowed chats"`
}

const (
	defaultMaxWait = 60 * time.Second
	maxMaxWait     = time.Hour
	// budgetMargin is what a wait's default budget adds to --max-wait for the requests around it.
	budgetMargin = 30 * time.Second
)

// parseMaxWait reads --max-wait: a duration with units, above zero and at most one hour.
func parseMaxWait(text string) (time.Duration, *errs.Error) {
	if text == "" {
		return defaultMaxWait, nil
	}
	wait, err := config.ParseDuration(text)
	if err != nil || wait <= 0 || wait > maxMaxWait {
		return 0, errs.Usagef("--max-wait %q must be a duration above zero and at most 1h, such as 90s or 5m.", text).
			WithDetail("flag", "--max-wait")
	}
	return wait, nil
}

// waitBudget is updates.wait's deadline for the whole call: max-wait plus 30 s unless the caller
// set a budget, which then may not be shorter than the wait.
func waitBudget(input any, configured time.Duration, explicit bool) (time.Duration, error) {
	maxWait, err := parseMaxWait(input.(updatesWaitInput).MaxWait)
	if err != nil {
		return 0, err
	}
	if !explicit {
		return maxWait + budgetMargin, nil
	}
	if configured < maxWait {
		return 0, errs.Usagef("--budget %s is shorter than --max-wait %s, so the wait could never finish.", configured, maxWait).
			WithHint("Raise --budget to at least --max-wait, or lower --max-wait.").
			WithDetail("flag", "--budget")
	}
	return configured, nil
}

// Reads returns the read-only domain commands.
func Reads() []*registry.Command {
	return []*registry.Command{
		registry.Object[botGetInput, telegram.Bot](registry.Spec{
			Name:        "bot.get",
			Description: "Show the bot this profile uses: its id and username.",
			Fields:      &registry.FieldSet{Default: telegram.BotDefault, Available: telegram.BotAvailable},
			Errors:      botErrors,
			Examples:    []registry.Example{{Argv: []string{"bot", "get"}, Description: "Which bot am I?"}},
		}, func(ctx context.Context, call *registry.Call, _ botGetInput) (shape.Value, error) {
			return call.Upstream.Do(ctx, &upstream.Request{Method: "POST", Path: "/getMe"})
		}),

		registry.Object[chatsGetInput, telegram.Chat](registry.Spec{
			Name:        "chats.get",
			Description: "Show one chat the bot can see: its type, title, and username.",
			Fields:      &registry.FieldSet{Default: telegram.ChatDefault, Available: telegram.ChatAvailable},
			Errors:      chatErrors,
			Unavailable: anyChatUnavailable(),
			Examples: []registry.Example{
				{Argv: []string{"chats", "get"}, Description: "The profile's default chat"},
				{Argv: []string{"chats", "get", "--chat", "-1001234567890"}, Description: "A group"},
			},
		}, func(ctx context.Context, call *registry.Call, in chatsGetInput) (shape.Value, error) {
			chat, err := ResolveChat(call, in.Chat, in.AllowAnyChat)
			if err != nil {
				return shape.Value{}, err
			}
			return call.Upstream.Do(ctx, &upstream.Request{Method: "POST", Path: "/getChat",
				Body: map[string]any{"chat_id": telegram.ChatParam(chat)}})
		}),

		registry.List[updatesListInput, telegram.Update](registry.Spec{
			Name:        "updates.list",
			Description: "List incoming updates waiting in the bot's queue, oldest first. Reading never removes them.",
			TimeWindow:  true,
			Fields:      &registry.FieldSet{Default: telegram.UpdateDefault, Available: telegram.UpdateAvailable},
			Sort:        "update_id asc",
			Limits:      &registry.Limits{Default: 25, Max: telegram.QueueWindow},
			Errors:      updatesErrors,
			Unavailable: anyChatUnavailable(),
			Examples: []registry.Example{
				{Argv: []string{"updates", "list"}, Description: "Updates from the last 24 hours, from the allowed chats"},
				{Argv: []string{"updates", "list", "--chat", "111111111", "--after", "812345", "--fields", "update_id,from.username,text"},
					Description: "What one chat sent after a known update"},
			},
		}, func(ctx context.Context, call *registry.Call, in updatesListInput) (registry.ListSource, error) {
			scope, err := ChatScope(call, in.Chat, in.AllowAnyChat, false)
			if err != nil {
				return nil, err
			}
			return &telegram.UpdatesSource{Upstream: call.Upstream, Chat: scope.Chat, Allowed: scope.Allowed,
				FilterAllowed: scope.FilterAllowed, After: in.After, Window: call.Window,
				AckAvailable: ackAvailable, AnyChatAvailable: anyChatAvailable}, nil
		}),

		registry.List[updatesWaitInput, telegram.Update](registry.Spec{
			Name: "updates.wait",
			Description: "Wait for a person's reply: returns the first matching message, or nothing after --max-wait. " +
				"Reading never removes updates.",
			Fields:      &registry.FieldSet{Default: telegram.UpdateDefault, Available: telegram.UpdateAvailable},
			Sort:        "update_id asc",
			Limits:      &registry.Limits{Default: 25, Max: telegram.QueueWindow},
			Errors:      updatesErrors,
			Unavailable: anyChatUnavailable(),
			Budget:      waitBudget,
			Examples: []registry.Example{
				{Argv: []string{"updates", "wait", "--after-message", "4021", "--max-wait", "5m"},
					Description: "The reply to a message just sent (4021 is the message_id the send returned)"},
				{Argv: []string{"updates", "wait", "--reply-to", "4021"},
					Description: "Only a message that replies to message 4021"},
			},
		}, func(ctx context.Context, call *registry.Call, in updatesWaitInput) (registry.ListSource, error) {
			maxWait, err := parseMaxWait(in.MaxWait)
			if err != nil {
				return nil, err
			}
			scope, scopeErr := ChatScope(call, in.Chat, in.AllowAnyChat, true)
			if scopeErr != nil {
				return nil, scopeErr
			}
			return &telegram.WaitSource{Upstream: call.Upstream, Scope: scope, AfterMessage: in.AfterMessage,
				ReplyTo: in.ReplyTo, MaxWait: maxWait, Poll: call.PollInterval, Now: call.Now, Sleep: call.Sleep,
				AckAvailable: ackAvailable, AnyChatAvailable: anyChatAvailable}, nil
		}),
	}
}
