package commands

// MutatingNames lists every mutating command, in every build. A read-only build has none of their
// handlers, but uses this list to refuse them with writes_disabled rather than usage (contract §11.3).
var MutatingNames = []string{"messages.send", "messages.send-document", "messages.send-photo", "updates.ack"}
