---
summary: Reading a card's attachments, comments, and checklists
---
## Why separate commands

`cards get` returns the card itself. Attachments, comments, and checklists live on their own
endpoints, so each has a list command that takes the card id or short link.

## Attachments

`{{tool}} cards attachments <card>` returns `id`, `name`, `url`, `date`, and `isUpload` (true for
an uploaded file, false for a link), oldest first. `--fields id,name,bytes,mimeType` adds size and
type.

## Comments

`{{tool}} cards comments <card>` returns the card's comments, newest first, with `id`, `date`, the
author as `memberCreator.username`, and the text as `data.text`. For the author's full name add
`--fields id,date,memberCreator.fullName,data.text`.

Comments have no time window: unlike `actions list`, the whole history is returned, and `--limit`
and `--cursor` page through it. Trello returns at most the 1000 newest comments of one card.
For comments across a board, use `actions list --type commentCard`.

## Checklists

`{{tool}} cards checklists <card>` returns each checklist in card order with its `checkItems`, each
holding `id`, `name`, `state` (`complete` or `incomplete`), and `pos`. A checklist with no items
has no `checkItems` key, because empty values are stripped by default. Use `--fields id,name` to
list the checklists without their items.
