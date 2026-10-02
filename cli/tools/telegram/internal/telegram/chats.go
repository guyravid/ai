package telegram

import (
	"strconv"
	"strings"
)

// CanonicalChat normalises a chat id for comparison: a number loses any leading zeros, and a public
// username is lower case, since Telegram treats usernames case-insensitively.
func CanonicalChat(chat string) string {
	if strings.HasPrefix(chat, "@") {
		return strings.ToLower(chat)
	}
	if number, err := strconv.ParseInt(chat, 10, 64); err == nil {
		return strconv.FormatInt(number, 10)
	}
	return chat
}

// ChatInSet reports whether chat is one of set.
func ChatInSet(chat string, set []string) bool {
	want := CanonicalChat(chat)
	for _, member := range set {
		if CanonicalChat(member) == want {
			return true
		}
	}
	return false
}

// ChatScope says which updates a command may see (plan, "Allowlist rule"). An explicit Chat keeps
// only that chat. Otherwise, when FilterAllowed is set, only chats in Allowed are kept (an empty
// Allowed keeps nothing). With neither, every chat is kept.
type ChatScope struct {
	Chat          string
	Allowed       []string
	FilterAllowed bool
}

// Narrow reports whether the scope drops any update by chat.
func (s ChatScope) Narrow() bool { return s.Chat != "" || s.FilterAllowed }

// Allowlisting reports whether the scope is the allowlist filter, whose drops are counted and
// reported, as opposed to an explicit --chat the caller asked for.
func (s ChatScope) Allowlisting() bool { return s.Chat == "" && s.FilterAllowed }

// Match reports whether an update's chat is in scope.
func (s ChatScope) Match(parsed Parsed) bool {
	switch {
	case s.Chat != "":
		return chatMatches(s.Chat, parsed)
	case s.FilterAllowed:
		for _, allowed := range s.Allowed {
			if chatMatches(allowed, parsed) {
				return true
			}
		}
		return false
	}
	return true
}

func chatMatches(chat string, parsed Parsed) bool {
	if strings.HasPrefix(chat, "@") {
		return parsed.ChatUsername != "" && strings.EqualFold("@"+parsed.ChatUsername, chat)
	}
	return parsed.ChatID != "" && CanonicalChat(parsed.ChatID) == CanonicalChat(chat)
}

// DroppedChats counts what the allowlist filter left out of a queue: updates, and the distinct chats
// they came from. Never the ids themselves.
type DroppedChats struct {
	Updates int
	chats   map[string]bool
}

func (d *DroppedChats) add(parsed Parsed) {
	d.Updates++
	if d.chats == nil {
		d.chats = map[string]bool{}
	}
	d.chats[parsed.ChatID] = true
}

// Chats is the number of distinct chats dropped. An update that carries no chat counts as one.
func (d *DroppedChats) Chats() int { return len(d.chats) }
