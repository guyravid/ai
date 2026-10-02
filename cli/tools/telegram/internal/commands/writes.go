//go:build !readonly

package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"unicode/utf8"

	"github.com/guyravid/ai/cli/tools/telegram/internal/config"
	"github.com/guyravid/ai/cli/tools/telegram/internal/errs"
	"github.com/guyravid/ai/cli/tools/telegram/internal/registry"
	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
	"github.com/guyravid/ai/cli/tools/telegram/internal/telegram"
	"github.com/guyravid/ai/cli/tools/telegram/internal/upstream"
)

// WritesEnabled reports whether this build includes write commands.
const WritesEnabled = true

// anyChatUnavailable lists the parameters this build does not offer; the full build offers all.
func anyChatUnavailable() []registry.Unavailable { return nil }

// maxTextFileBytes bounds --text-file so a stray path cannot pull an enormous file into memory.
// Telegram accepts at most 4096 characters, so this is far above anything it would take.
const maxTextFileBytes = 1 << 20

var (
	writeErrors = []errs.Code{errs.Usage, errs.Config, errs.Auth, errs.Refused, errs.NotFound, errs.Validation,
		errs.ChatUnreachable, errs.RateLimited, errs.Timeout, errs.Network, errs.Upstream}
	ackErrors = []errs.Code{errs.Usage, errs.Config, errs.Auth, errs.Conflict, errs.RateLimited, errs.Timeout,
		errs.Network, errs.Upstream}
)

type sendInput struct {
	Chat         string `json:"chat,omitempty" jsonschema:"Chat id or public @username; defaults to the profile's default_chat"`
	Text         string `json:"text,omitempty" jsonschema:"Message text; give this or text_file"`
	TextFile     string `json:"text_file,omitempty" jsonschema:"File holding the message text, UTF-8; keeps long text out of argv"`
	ParseMode    string `json:"parse_mode,omitempty" jsonschema:"Formatting: none, html, or markdownv2; defaults to the PARSE_MODE setting"`
	ReplyTo      int64  `json:"reply_to,omitempty" jsonschema:"Reply to this message_id in the same chat"`
	Silent       bool   `json:"silent,omitempty" jsonschema:"Deliver without a notification sound"`
	AllowAnyChat bool   `json:"allow_any_chat,omitempty" jsonschema:"Send to a chat outside the profile's allowed chats"`
}

type sendFileInput struct {
	Chat         string `json:"chat,omitempty" jsonschema:"Chat id or public @username; defaults to the profile's default_chat"`
	File         string `json:"file" jsonschema:"Local file to upload; symlinks are resolved and the path is shown in the preview"`
	Caption      string `json:"caption,omitempty" jsonschema:"Caption, up to 1024 characters"`
	ParseMode    string `json:"parse_mode,omitempty" jsonschema:"Caption formatting: none, html, or markdownv2; defaults to the PARSE_MODE setting"`
	ReplyTo      int64  `json:"reply_to,omitempty" jsonschema:"Reply to this message_id in the same chat"`
	Silent       bool   `json:"silent,omitempty" jsonschema:"Deliver without a notification sound"`
	AllowAnyChat bool   `json:"allow_any_chat,omitempty" jsonschema:"Send to a chat outside the profile's allowed chats"`
}

type ackInput struct {
	Through int64 `json:"through" jsonschema:"Delete every update up to and including this update_id"`
}

// apiParseMode maps the setting's value to what the Bot API takes; none sends no parse_mode.
func apiParseMode(flag string, call *registry.Call) string {
	mode := flag
	if mode == "" {
		mode = call.ParseMode
	}
	switch mode {
	case "html":
		return "HTML"
	case "markdownv2":
		return "MarkdownV2"
	}
	return ""
}

// resolveFile makes a path absolute, follows symlinks, and requires a regular file, so what is
// previewed is the file that would be read.
func resolveFile(path, flag string) (string, *errs.Error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", errs.Usagef("%s %q is not a usable path.", flag, path).WithDetail("flag", flag)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", errs.Usagef("The file %s does not exist or cannot be read.", absolute).
			WithDetail("flag", flag).WithDetail("file", absolute)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", errs.Usagef("%s must name a regular file; %s is not one.", flag, resolved).
			WithDetail("flag", flag).WithDetail("file", resolved)
	}
	return resolved, nil
}

// readText returns the message text from --text or --text-file, exactly one of them.
func readText(in sendInput) (string, *errs.Error) {
	switch {
	case in.Text != "" && in.TextFile != "":
		return "", errs.Usagef("--text and --text-file are mutually exclusive.").
			WithDetail("flags", []string{"--text", "--text-file"})
	case in.Text == "" && in.TextFile == "":
		return "", errs.Usagef("messages send requires --text <text> or --text-file <path>.").
			WithHint("telegram describe messages.send").WithDetail("missing", "text")
	case in.Text != "":
		return in.Text, nil
	}
	resolved, err := resolveFile(in.TextFile, "--text-file")
	if err != nil {
		return "", err
	}
	file, openErr := os.Open(resolved)
	if openErr != nil {
		return "", errs.Usagef("The file %s cannot be opened.", resolved).WithDetail("file", resolved)
	}
	defer file.Close()
	content, readErr := io.ReadAll(io.LimitReader(file, maxTextFileBytes+1))
	if readErr != nil {
		return "", errs.Usagef("The file %s could not be read.", resolved).WithDetail("file", resolved)
	}
	if len(content) > maxTextFileBytes {
		return "", errs.Usagef("The file %s is larger than %d bytes, far beyond Telegram's 4096-character limit.", resolved, maxTextFileBytes).
			WithDetail("file", resolved)
	}
	if !utf8.Valid(content) {
		return "", errs.Usagef("The file %s is not valid UTF-8 text.", resolved).WithDetail("file", resolved)
	}
	return string(content), nil
}

// sendResult reduces the Message Telegram returns to the record an agent needs.
func sendResult(_ any, upstream shape.Value) (shape.Value, error) {
	return telegram.SentRecord(upstream), nil
}

func ackResult(input any, upstream shape.Value) (shape.Value, error) {
	return telegram.AckRecord(input.(ackInput).Through, upstream), nil
}

// uploadRequest builds a multipart send. The file is read only when the write is confirmed.
func uploadRequest(call *registry.Call, method, field string, in sendFileInput) (*upstream.Request, error) {
	chat, err := ResolveChat(call, in.Chat, in.AllowAnyChat)
	if err != nil {
		return nil, err
	}
	resolved, fileErr := resolveFile(in.File, "--file")
	if fileErr != nil {
		return nil, fileErr
	}
	fields := map[string]string{"chat_id": chat}
	set := func(key, value string) {
		if value != "" {
			fields[key] = value
		}
	}
	set("caption", in.Caption)
	set("parse_mode", apiParseMode(in.ParseMode, call))
	if in.Silent {
		fields["disable_notification"] = "true"
	}
	if in.ReplyTo != 0 {
		// TODO(M9): confirm reply_parameters (Bot API 7.0) against a live response.
		fields["reply_parameters"] = `{"message_id":` + strconv.FormatInt(in.ReplyTo, 10) + `}`
	}
	return &upstream.Request{Method: "POST", Path: method, Write: true,
		Upload: &upstream.Upload{Field: field, Path: resolved, Name: filepath.Base(resolved), Fields: fields}}, nil
}

// Writes returns the mutating commands. Each only builds its request; --confirm, --dry-run, and the
// refusal preview are the substrate's (contract §11.2). None is ever retried (§11.4).
func Writes() []*registry.Command {
	return []*registry.Command{
		registry.Write[sendInput, telegram.SentMessage](registry.Spec{
			Name:        "messages.send",
			Description: "Send a text message to a chat. Returns its message_id, which updates.wait takes to find the reply.",
			Enums:       map[string][]string{"parse_mode": config.ParseModes},
			Errors:      writeErrors,
			Unavailable: anyChatUnavailable(),
			Result:      sendResult,
			Examples: []registry.Example{
				{Argv: []string{"messages", "send", "--text", "Deploy finished. Roll back?", "--confirm"}, Description: "Message the default chat"},
				{Argv: []string{"messages", "send", "--text-file", "./report.txt", "--chat", "111111111", "--confirm"},
					Description: "Send composed text from a file"},
			},
		}, func(ctx context.Context, call *registry.Call, in sendInput) (*upstream.Request, error) {
			chat, err := ResolveChat(call, in.Chat, in.AllowAnyChat)
			if err != nil {
				return nil, err
			}
			text, textErr := readText(in)
			if textErr != nil {
				return nil, textErr
			}
			body := map[string]any{"chat_id": telegram.ChatParam(chat), "text": text}
			if mode := apiParseMode(in.ParseMode, call); mode != "" {
				body["parse_mode"] = mode
			}
			if in.Silent {
				body["disable_notification"] = true
			}
			if in.ReplyTo != 0 {
				// TODO(M9): confirm reply_parameters (Bot API 7.0) against a live response.
				body["reply_parameters"] = map[string]any{"message_id": in.ReplyTo}
			}
			return &upstream.Request{Method: "POST", Path: "/sendMessage", Body: body, Write: true}, nil
		}),

		registry.Write[sendFileInput, telegram.SentMessage](registry.Spec{
			Name:        "messages.send-document",
			Description: "Upload a local file to a chat as a document, up to 50 MB. Any readable file can be sent.",
			Enums:       map[string][]string{"parse_mode": config.ParseModes},
			Errors:      writeErrors,
			Unavailable: anyChatUnavailable(),
			Result:      sendResult,
			Examples: []registry.Example{{Argv: []string{"messages", "send-document", "--file", "./report.pdf", "--caption", "Weekly report", "--confirm"},
				Description: "Send a file"}},
		}, func(ctx context.Context, call *registry.Call, in sendFileInput) (*upstream.Request, error) {
			return uploadRequest(call, "/sendDocument", "document", in)
		}),

		registry.Write[sendFileInput, telegram.SentMessage](registry.Spec{
			Name:        "messages.send-photo",
			Description: "Upload a local image to a chat as a photo, up to 10 MB. Any readable file can be sent.",
			Enums:       map[string][]string{"parse_mode": config.ParseModes},
			Errors:      writeErrors,
			Unavailable: anyChatUnavailable(),
			Result:      sendResult,
			Examples: []registry.Example{{Argv: []string{"messages", "send-photo", "--file", "./graph.png", "--caption", "Latency", "--confirm"},
				Description: "Send an image"}},
		}, func(ctx context.Context, call *registry.Call, in sendFileInput) (*upstream.Request, error) {
			return uploadRequest(call, "/sendPhoto", "photo", in)
		}),

		registry.Write[ackInput, telegram.AckResult](registry.Spec{
			Name:        "updates.ack",
			Description: "Permanently delete updates up to an update_id, for every reader of the bot. Cannot be undone.",
			Destructive: true,
			Idempotent:  true,
			Errors:      ackErrors,
			Result:      ackResult,
			Examples: []registry.Example{{Argv: []string{"updates", "ack", "--through", "812345", "--confirm"},
				Description: "Drop everything up to update 812345 once it has been handled"}},
		}, func(ctx context.Context, call *registry.Call, in ackInput) (*upstream.Request, error) {
			if in.Through < 0 {
				return nil, errs.Usagef("--through must be an update_id, which is never negative.").WithDetail("flag", "--through")
			}
			// The only request in this tool that sends offset: it confirms every update below it
			// for every consumer of the bot (plan, trap 2). limit=1 and timeout=0 keep the reply small.
			return &upstream.Request{Method: "POST", Path: "/getUpdates", Write: true,
				Body: map[string]any{"offset": in.Through + 1, "limit": 1, "timeout": 0}}, nil
		}),
	}
}
