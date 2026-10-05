package telegram

import (
	"encoding/json"
	"time"

	"github.com/guyravid/ai/cli/tools/telegram/internal/shape"
)

// SentMessage is the record a send returns. The raw Message is much larger; an agent needs the
// message_id to pass to `updates wait`.
type SentMessage struct {
	MessageID json.Number `json:"message_id"`
	Date      string      `json:"date,omitempty"`
	Chat      *UpdateChat `json:"chat,omitempty"`
	Text      string      `json:"text,omitempty"`
	Caption   string      `json:"caption,omitempty"`

	ReplyToMessageID int64           `json:"reply_to_message_id,omitempty"`
	Document         *UpdateDocument `json:"document,omitempty"`
	Photo            string          `json:"photo,omitempty"`
}

// SentDefault is every field of a send result; there is no --fields on a write.
var SentDefault = []string{"message_id", "date", "chat.id", "chat.type", "text", "caption",
	"reply_to_message_id", "document.file_id", "document.file_unique_id", "document.file_name", "document.mime_type",
	"document.file_size", "photo"}

// AckResult is the data of updates.ack.
type AckResult struct {
	AckedThrough int64 `json:"acked_through"`
	HasMore      bool  `json:"has_more"`
}

// SentRecord reduces the Message that sendMessage, sendDocument and sendPhoto return to a flat
// record, with the date as RFC 3339 UTC (contract §8.5).
func SentRecord(raw shape.Value) shape.Value {
	var record []shape.Field
	if id, ok := raw.Get("message_id"); ok && !id.IsNull() {
		record = append(record, shape.Field{Key: "message_id", Value: id})
	}
	if date, ok := unixDate(raw); ok {
		record = append(record, shape.Field{Key: "date", Value: shape.String(date.Format(time.RFC3339))})
	}
	if chat, ok := raw.Get("chat"); ok && chat.Kind == shape.Object {
		if fields, _, _ := pick(chat, "id", "type", "title", "username"); len(fields) > 0 {
			record = append(record, shape.Field{Key: "chat", Value: shape.NewObject(fields...)})
		}
	}
	for _, key := range []string{"text", "caption"} {
		if value, ok := raw.Get(key); ok && !value.IsNull() {
			record = append(record, shape.Field{Key: key, Value: value})
		}
	}
	if reply, ok := raw.Get("reply_to_message"); ok {
		if value, ok := reply.Get("message_id"); ok && !value.IsNull() {
			record = append(record, shape.Field{Key: "reply_to_message_id", Value: value})
		}
	}
	if document, ok := documentRecord(raw); ok {
		record = append(record, shape.Field{Key: "document", Value: document})
	}
	if fileID := largestPhoto(raw); fileID != "" {
		record = append(record, shape.Field{Key: "photo", Value: shape.String(fileID)})
	}
	return shape.NewObject(record...)
}

// AckRecord is the result of an acknowledgement: how far it went, and whether the queue still shows
// another update after it.
func AckRecord(through int64, upstream shape.Value) shape.Value {
	return shape.NewObject(
		shape.Field{Key: "acked_through", Value: shape.Int(through)},
		shape.Field{Key: "has_more", Value: shape.Bool(upstream.Kind == shape.Array && len(upstream.Items) > 0)},
	)
}
