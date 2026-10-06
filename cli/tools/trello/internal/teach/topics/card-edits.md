---
summary: Renaming cards
writes: true
---
## Renaming

`{{tool}} cards rename <card> --name "New title" --confirm` changes a card's title and returns the
card. It is idempotent. The name must not be empty and must be a single line; otherwise it is a
validation error and nothing is sent.

Use `cards update` to change the description, not the title.
