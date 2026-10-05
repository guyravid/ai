---
summary: Creating checklists, adding items, and ticking them
writes: true
---
## Workflow

1. `{{tool}} cards add-checklist <card> --name "Release steps" --confirm` creates an empty
   checklist and returns its `id`.
2. `{{tool}} checklists add-item <checklist-id> --name "Tag the release" --confirm` adds an item.
   Add `--checked` to create it already complete.
3. `{{tool}} cards check-item <card> --check-item <item-id> --state complete --confirm` ticks an
   item. `--state incomplete` unticks it. The command is idempotent.

Find checklist and item ids with `{{tool}} cards checklists <card>`.

## Notes

- `add-checklist` and `add-item` create something new on every confirmed run. Running one twice
  gives two checklists or two items; check with `cards checklists` before retrying.
- `check-item` takes the card id as well as the item id: Trello addresses an item through its card.
- `--state` accepts only `complete` or `incomplete`; anything else is a usage error and nothing is
  sent.
- There is no command to rename, reorder, or delete checklists or items.
