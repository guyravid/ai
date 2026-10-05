---
summary: Attaching links and local files to cards, and the risks of uploads
writes: true
---
## Links

`{{tool}} cards attach-url <card> --url https://example.com/spec --confirm` attaches a link.
`--name` sets the label shown on the card.

## Files

`{{tool}} cards attach-file <card> --file ./report.pdf --confirm` uploads a local file.

- Run it once without `--confirm` first. The refusal's preview shows the resolved path, the name
  sent, and the size, never the content.
- The file is read only when the write is confirmed. A missing or unreadable file is a usage error,
  and nothing is sent.
- Trello rejects files above its plan's size limit with a validation error.

## Listing and removing

`{{tool}} cards attachments <card>` lists a card's attachments, oldest first. `isUpload` is true
for uploaded files and false for links.

`{{tool}} cards detach <card> --attachment <attachment-id> --confirm` removes one. It is
permanent: an uploaded file is deleted from Trello, not just hidden. Take the id from
`cards attachments` and confirm it is the one meant before running with `--confirm`.

## Risk

`attach-file` can upload any file this process can read. Only confirm uploads of files you were
asked to share. Operators who do not want this ability should run the read-only build.
