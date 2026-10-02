---
summary: Board, list, and card identifiers, and how to find them
---
## Two kinds of id

Every Trello object has a 24-character hex id, such as `5f2a1b3c4d5e6f7a8b9c0d1e`. Boards and cards
also have a short link: the 8-character segment of their URL. In `https://trello.com/c/Ab12Cd34/…`
the card's short link is `Ab12Cd34`.

`boards get`, `cards get`, and the `--board` parameter accept either form. `--list-id` needs the full
list id; lists have no short link.

## Finding a board

1. `{{tool}} boards list --fields id,name` lists the open boards this token can see.
2. Add `--filter all` to include closed boards.

## Finding a list

`{{tool}} lists list --board <board>` returns the board's lists in board order, left to right. Use
the `id` of a list as `--list-id` elsewhere.

## Finding a card

- On a known board: `{{tool}} cards list --board <board> --fields id,name,idList`.
- Anywhere: `{{tool}} cards search --query "<words>"`.
- From a URL: take the short link and run `{{tool}} cards get <shortLink>`.
