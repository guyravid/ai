---
summary: Query idioms for cards search, and its limits
---
## Query syntax

`--query` takes Trello's own search syntax. Words match card titles and descriptions. Operators
narrow the search:

| Operator | Meaning |
|---|---|
| `label:bug` | Has the label named bug |
| `list:"In Progress"` | In a list with that name |
| `member:me` | Assigned to the token's member |
| `is:open`, `is:archived` | Open or archived cards |
| `due:week`, `due:overdue` | Due this week, or overdue |
| `created:7`, `edited:1` | Created or edited in the last N days |

Quote multi-word values. Combine operators with spaces; they are ANDed.

## Limits

- One search returns at most 1000 cards. If `meta.page.total` is 1000, more may exist: narrow the
  query rather than paging.
- Results are ordered by the tool, most recently active first, not by Trello's relevance.
- `--board <id>` restricts the search to one board.

## Examples

- `{{tool}} cards search --query "label:bug is:open" --fields id,name,idList`
- `{{tool}} cards search --query "due:overdue member:me"`
