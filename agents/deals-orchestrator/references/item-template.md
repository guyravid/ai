# Item template (card description)

Every tracked item stores its spec in the card description using this exact shape. The `deals` CLI parses and writes it; agents should produce specs as JSON for `deals items create|update --spec-file`, not hand-write this markdown.

```
## Deal Spec
- **Type:** general
- **Status:** draft
- **Category:** Bikes
- **Use:** Winter commuting and trails
- **Condition:** used-ok
- **Target price:** 150 CAD
- **Max price:** 200 CAD
- **Location:** Ottawa, ON
- **Pickup radius km:** 50
- **Shipping OK:** no
- **Product name:**
- **Model number:**
- **UPC/EAN:**
- **ASIN:**

### Must have
- Disc brakes
### Nice to have
- Rack mounts
### Deal breakers
- Frame cracks
### Fit
- Rider height: 1.81 m
### Acceptable variants
- 2023 and 2024 model years

## Search brief
- **Keywords:** hardtail mtb, winter bike
- **Synonyms:** mountain bike, commuter
- **Exclude:** kids, parts only
- **Updated:** 2026-10-05
### Rules
- Frame size M or L only

## Notes
Free text written by the user. Preserved verbatim.
```

## Fields

| Field | Values | Applies to |
|---|---|---|
| Type | `general` (a kind of product) or `specific` (one exact product) | all |
| Status | `draft` or `ready`; only `ready` items with a valid spec are searched | all |
| Category | Free text; matches a card in the Source Library list | all |
| Use | What the item is for | all |
| Condition | `new`, `used-ok`, `used-only` | all |
| Target price / Max price | `<amount> <ISO currency>`, e.g. `150 CAD` | all |
| Location | Free text | all |
| Pickup radius km | Number | all |
| Shipping OK | `yes` or `no` | all |
| Product name, Model number | Free text | specific only |
| UPC/EAN, ASIN | Free text, optional | specific only |
| Acceptable variants | Bullet list | specific only (section omitted for general) |

Specific specs emit the four specific-only fields and the `Acceptable variants` section; general specs omit them.

## Search-ready rules

An item is search-ready only when Status is `ready` AND:

- General: category, use, condition, target price, max price, pickup radius > 0 or shipping ok = yes, and at least one must-have.
- Specific: product name, model number, condition, target price, max price.
- Both: prices are positive, share a currency, and max is not below target.

## Parsing and preservation

- Labels are matched case-insensitively and whitespace-tolerant (`**Target Price**:` and `**target price:**` both work).
- A description with no `## Deal Spec` section parses to no spec. Nothing is thrown.
- Content the parser does not recognise is preserved on write: text before the first `##` heading, unknown `##` sections, unknown field lines, unknown `###` sections, and a field line whose value is invalid (for example `Condition: mint`) are all written back unchanged.
- Within a list section, a non-bullet line is kept as a bullet item.
- List-valued brief fields (Keywords, Synonyms, Exclude) are comma-separated; items cannot contain commas.
- `## Notes` holds the user's free text and is never rewritten except through `deals items update --notes-file`.
