// Package telegram is the adapter for the Telegram Bot API: how a response is unwrapped, how its
// failures map onto the contract's codes, and the shapes of the records this tool returns.
//
// Field and method names come from the Bot API documentation (https://core.telegram.org/bots/api).
// None has been confirmed against a live response yet; every one that is not certain is marked
// TODO(M9), to be settled by the live smoke test.
package telegram

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
	"github.com/guyravid/ai/cli/tools/telegram/internal/upstream"
)

const (
	// maxDescriptionChars bounds upstream text copied into error.details (contract §3.3).
	maxDescriptionChars = 512
	toolName            = "telegram"
)

// Protocol implements upstream.Protocol for the Bot API.
type Protocol struct{}

// response is the Bot API envelope: {ok, result} or {ok:false, error_code, description, parameters}.
type response struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter *int64 `json:"retry_after"`
	} `json:"parameters"`
}

// Result extracts result from a 2xx body. A 2xx body that says ok:false is mapped like the status
// it names, since the API documents that the two agree but nothing forces it.
func (Protocol) Result(status int, body []byte) (shape.Value, *errs.Error) {
	var parsed response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return shape.Value{}, errs.New(errs.Upstream, "Telegram returned a response that is not JSON.").
			WithDetail("upstream_status", status)
	}
	if !parsed.OK {
		code := parsed.ErrorCode
		if code < 100 {
			code = status
		}
		return shape.Value{}, mapError(code, parsed)
	}
	value, err := shape.Parse(parsed.Result)
	if err != nil {
		return shape.Value{}, errs.New(errs.Upstream, "Telegram returned a response without a result.").
			WithDetail("upstream_status", status)
	}
	return value, nil
}

// StatusError maps a non-2xx response (plan, "Error mapping").
func (Protocol) StatusError(status int, body []byte, wait time.Duration, _ *upstream.Request) *errs.Error {
	var parsed response
	if err := json.Unmarshal(body, &parsed); err != nil {
		// Not the API's own JSON (a proxy's error page, say): keep a bounded excerpt of the text.
		parsed = response{Description: strings.TrimSpace(string(body))}
	}
	if parsed.Parameters.RetryAfter == nil && wait > 0 {
		seconds := int64(wait / time.Second)
		parsed.Parameters.RetryAfter = &seconds
	}
	return mapError(status, parsed)
}

func mapError(status int, parsed response) *errs.Error {
	description := limit(parsed.Description)
	lower := strings.ToLower(parsed.Description)
	var err *errs.Error
	switch {
	case status == 400 && strings.Contains(lower, "chat not found"):
		err = errs.New(errs.NotFound, "Telegram has no chat with that id that this bot can see.").
			WithHint(toolName + " updates list --fields update_id,chat.id,chat.type,from.username")
	case status == 400 && (strings.Contains(lower, "message to reply not found") ||
		strings.Contains(lower, "message to be replied not found") ||
		strings.Contains(lower, "replied message not found")):
		err = errs.New(errs.NotFound, "The message being replied to does not exist in that chat.")
	case status == 400 || status == 422:
		err = errs.New(errs.Validation, "Telegram rejected the request: %s.", oneLine(description)).
			WithHint("Message text is 1-4096 characters after formatting (captions 0-1024). If the error mentions entities, check --parse-mode.")
	case status == 401:
		err = errs.New(errs.Auth, "Telegram rejected the bot token.").WithHint(toolName + " doctor")
	case status == 403:
		err = errs.New(errs.ChatUnreachable, "The bot cannot reach that chat: it is blocked, was never started, or was removed.").
			WithHint("The recipient must open the bot in Telegram and press Start; for a group, add the bot again.")
	case status == 404:
		// The API answers an unknown or malformed token with 401; 404 means the path itself is wrong.
		err = errs.New(errs.Auth, "Telegram answered 404 for this request; the bot token is malformed or unknown.").
			WithHint(toolName + " doctor")
	case status == 409:
		err = errs.New(errs.Conflict, "Another consumer is reading this bot's updates, or a webhook is set on it.").
			WithHint("Only one getUpdates consumer can run at a time: stop the other poller, or remove the bot's webhook, then retry.")
	case status == 413:
		err = errs.New(errs.Validation, "Telegram rejected the request as too large: %s.", oneLine(description)).
			WithHint("Documents are limited to 50 MB and photos to 10 MB.")
	case status == 429:
		err = errs.New(errs.RateLimited, "Telegram is rate limiting this bot.")
		if parsed.Parameters.RetryAfter != nil {
			err.WithRetryAfter(*parsed.Parameters.RetryAfter * 1000)
		}
	default:
		err = errs.New(errs.Upstream, "Telegram returned %d.", status)
	}
	err.WithDetail("upstream_status", status)
	if description != "" {
		err.WithDetail("upstream_description", description)
	}
	return err
}

// limit cuts upstream text to 512 characters (contract §3.3).
func limit(text string) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= maxDescriptionChars {
		return text
	}
	return string([]rune(text)[:maxDescriptionChars])
}

func oneLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	line = strings.TrimRight(line, ".")
	if line == "" {
		return "no reason given"
	}
	return line
}

// ChatParam is a chat id as the Bot API wants it: a JSON number when it is one, otherwise the
// string (a public @username). Ids stay strings everywhere else in this tool.
func ChatParam(chat string) any {
	if number, err := strconv.ParseInt(chat, 10, 64); err == nil {
		return number
	}
	return chat
}
