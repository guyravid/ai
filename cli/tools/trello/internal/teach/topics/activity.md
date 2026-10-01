---
summary: Reading board activity with actions list
---
## The time window

`actions list` has a time dimension. Without `--since` it searches only the last 24 hours, and
`meta.window.source` says `default`. Pass `--since -7d` (or a date) to look further back.

## Filtering by type

`--type` takes one or more Trello action types, comma-separated. Common ones:

| Type | Meaning |
|---|---|
| `commentCard` | A comment was added |
| `createCard` | A card was created |
| `updateCard` | A card changed: moved, renamed, due date, description, and more |
| `addMemberToCard` | Someone was assigned |
| `addAttachmentToCard` | An attachment was added |

## What an action holds

The default fields are `id`, `type`, `date`, and `idMemberCreator`. The details are in `data`, whose
shape depends on the type: add `--fields id,type,date,data.*` to see them. For comments, the text is
at `data.text`; the card is at `data.card`.

## Everything in a range

`{{tool}} actions list --board <board> --since -30d --all --fields id,type,date,data.*` collects the
whole range once. Read it with `{{tool}} dataset read <id>`, or process the JSON Lines file directly.
