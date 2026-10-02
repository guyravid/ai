package telegram

import "encoding/json"

// These types exist to derive describe's outputSchema. Records themselves pass through as ordered
// JSON, so a field Telegram adds later is not lost, and --fields decides what is shown.

// Bot is the getMe result, a User. TODO(M9): confirm each field against a live response.
type Bot struct {
	ID                      json.Number `json:"id"`
	IsBot                   bool        `json:"is_bot,omitempty"`
	FirstName               string      `json:"first_name,omitempty"`
	LastName                string      `json:"last_name,omitempty"`
	Username                string      `json:"username,omitempty"`
	CanJoinGroups           bool        `json:"can_join_groups,omitempty"`
	CanReadAllGroupMessages bool        `json:"can_read_all_group_messages,omitempty"`
	SupportsInlineQueries   bool        `json:"supports_inline_queries,omitempty"`
}

// BotDefault and BotAvailable are the fields of bot.get.
var (
	BotDefault   = []string{"id", "username", "first_name", "can_join_groups", "can_read_all_group_messages"}
	BotAvailable = []string{"id", "is_bot", "first_name", "last_name", "username", "can_join_groups",
		"can_read_all_group_messages", "supports_inline_queries"}
)

// Chat is the getChat result. TODO(M9): description, invite_link and is_forum are documented on
// Chat / ChatFullInfo but not yet confirmed against a live response.
type Chat struct {
	ID         json.Number `json:"id"`
	Type       string      `json:"type"`
	Title      string      `json:"title,omitempty"`
	Username   string      `json:"username,omitempty"`
	FirstName  string      `json:"first_name,omitempty"`
	LastName   string      `json:"last_name,omitempty"`
	IsForum    bool        `json:"is_forum,omitempty"`
	Desc       string      `json:"description,omitempty"`
	InviteLink string      `json:"invite_link,omitempty"`
}

// ChatDefault and ChatAvailable are the fields of chats.get.
var (
	ChatDefault   = []string{"id", "type", "title", "username", "first_name"}
	ChatAvailable = []string{"id", "type", "title", "username", "first_name", "last_name", "is_forum",
		"description", "invite_link"}
)

// Update is one flattened update (see Flatten).
type Update struct {
	UpdateID         int64           `json:"update_id"`
	Type             string          `json:"type"`
	Date             string          `json:"date,omitempty"`
	Chat             *UpdateChat     `json:"chat,omitempty"`
	From             *UpdateFrom     `json:"from,omitempty"`
	MessageID        int64           `json:"message_id,omitempty"`
	Text             string          `json:"text,omitempty"`
	Caption          string          `json:"caption,omitempty"`
	ReplyToMessageID int64           `json:"reply_to_message_id,omitempty"`
	Document         *UpdateDocument `json:"document,omitempty"`
	Photo            string          `json:"photo,omitempty"`
}

type UpdateChat struct {
	ID       json.Number `json:"id"`
	Type     string      `json:"type,omitempty"`
	Title    string      `json:"title,omitempty"`
	Username string      `json:"username,omitempty"`
}

type UpdateFrom struct {
	ID        json.Number `json:"id"`
	Username  string      `json:"username,omitempty"`
	FirstName string      `json:"first_name,omitempty"`
}

type UpdateDocument struct {
	FileName string `json:"file_name,omitempty"`
}

// UpdateDefault and UpdateAvailable are the fields of updates.list.
var (
	UpdateDefault   = []string{"update_id", "type", "date", "chat.id", "from.username", "message_id", "text"}
	UpdateAvailable = []string{"update_id", "type", "date", "chat.id", "chat.type", "chat.title", "chat.username",
		"from.id", "from.username", "from.first_name", "message_id", "text", "caption", "reply_to_message_id",
		"document.file_name", "photo"}
)
