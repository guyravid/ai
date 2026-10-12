package commands

// MutatingNames lists every mutating command, in every build. A read-only build has none of their
// handlers, but uses this list to refuse them with writes_disabled rather than usage (contract §11.3).
var MutatingNames = []string{
	"cards.add-checklist",
	"cards.archive",
	"cards.attach-file",
	"cards.attach-url",
	"cards.check-item",
	"cards.comment",
	"cards.create",
	"cards.delete",
	"cards.detach",
	"cards.move",
	"cards.rename",
	"cards.update",
	"checklists.add-item",
}
