# Apple Notes HTML Structure Reference

Apple Notes stores and returns note bodies as HTML. This reference is derived from
ground-truth inspection of a note with every paragraph style and font option applied.

---

## Paragraph Styles → HTML

Notes has a **read/write asymmetry** for heading styles: what you write with `<h2>`/`<h3>`
is normalized by Notes internally and read back in a different form. Both forms render
identically in the Notes UI — just use the write form when generating HTML.

| Notes UI Style | Write (generate this) | Read (Notes returns this) |
|---------------|----------------------|--------------------------|
| Title | **Omit entirely** — Notes manages via `name` property | `<div><h1>text</h1></div>` for UI-created notes; `<div>text</div>` for AppleScript-created |
| Heading | `<div><h2>text</h2></div>` | UI-created: `<div><h2>text</h2></div>` · AppleScript-created: `<div><b><span style="font-size: 18px">text</span></b></div>` |
| Subheading | `<div><h3>text</h3></div>` | UI-created: `<div><h3>text</h3></div>` · AppleScript-created: `<div><b>text</b></div>` |
| Body | `<div>text</div>` | `<div>text</div>` |
| Monostyled | `<div><tt>text</tt></div>` | `<div><font face="Courier"><tt>text</tt></font></div>` |
| Bulleted List | `<ul><li>item</li><li>item</li></ul>` | Same. **Always add `<div><br></div>` between adjacent lists of different types** or Notes merges them into one `<ul>`. |
| Dashed List | `<ul class="Apple-dash-list"><li>item</li></ul>` | Same |
| Numbered List | `<ol><li>item</li><li>item</li></ol>` | Same |
| Block Quote | `<div>text</div>` | **Indistinguishable from Body** — formatting is stored internally and not exposed via the `body` property. |

When parsing a note for editing, treat both Heading forms as section boundaries:
- `<div><h2>` — UI-created heading
- `<div><b><span style="font-size: 18px">` — AppleScript-created heading

### Empty line
```html
<div><br></div>
```

---

## Font Styles → HTML

Font styles are independent of paragraph style and can be combined with any of the above.

| Style | HTML |
|-------|------|
| Bold | `<b>text</b>` |
| Italic | `<i>text</i>` |
| Underline | `<u>text</u>` |
| Strikethrough | `<strike>text</strike>` |
| Combined | `<b><i><u><strike>text</strike></u></i></b>` |
| Font size | `<span style="font-size: 20px">text</span>` |
| Custom font | `<font face="SnellRoundhand">text</font>` |
| **Highlight** | Not in HTML — lost when reading/writing via AppleScript |
| **Superscript** | Not in HTML — lost when reading/writing via AppleScript |
| **Subscript** | Not in HTML — lost when reading/writing via AppleScript |

Font styles wrap the text inside the paragraph div:
```html
<div><b>Bold body text</b></div>
<div><b><h2>Bold heading</h2></b></div>
<div><i><h3>Italic subheading</h3></i></div>
```

---

## Tables

Tables are wrapped in `<object>` with Apple-specific inline styles:

```html
<div><object><table cellspacing="0" cellpadding="0" style="border-collapse: collapse; direction: ltr">
<tbody>
  <tr>
    <td valign="top" style="border-style: solid; border-width: 1.0px 1.0px 1.0px 1.0px; border-color: #ccc; padding: 3.0px 5.0px 3.0px 5.0px; min-width: 70px"><div>Cell text</div></td>
    <td valign="top" style="border-style: solid; border-width: 1.0px 1.0px 1.0px 1.0px; border-color: #ccc; padding: 3.0px 5.0px 3.0px 5.0px; min-width: 70px"><div>Cell text</div></td>
  </tr>
</tbody>
</table></object><br></div>
```

---

## Complete Example

```html
<div><h2>Section Heading</h2></div>
<div><br></div>
<div><h3>Subheading</h3></div>
<div>Body paragraph text.</div>
<div><b>Bold body text.</b></div>
<div><br></div>
<div><tt>Monospaced line</tt></div>
<div><br></div>
<ul>
<li>Bullet item one</li>
<li>Bullet item two</li>
</ul>
<div><br></div>
<ul class="Apple-dash-list">
<li>Dash item one</li>
<li>Dash item two</li>
</ul>
<div><br></div>
<ol>
<li>Numbered item one</li>
<li>Numbered item two</li>
</ol>
```

---

## Key Limitations

- **Paragraph style metadata is always stored as "Body"** when content is written via
  `set body` in AppleScript. `<h2>` renders visually as a heading but the right-click
  Paragraph Styles menu will show "Body" checked, not "Heading". This is a fundamental
  AppleScript API limitation — fixing it requires UI automation (System Events), which
  is out of scope. The visual appearance is correct.

- **Do not add a title heading as the first body element.** Notes auto-generates a
  note-name div at the top of the body. Adding an `<h2>` subtitle immediately after
  creates a double-title effect. Start the body with the description or first section
  heading directly — the `name` property already serves as the visible title.

- **Block Quote, Highlight, Superscript, Subscript** are not round-trippable via the
  `body` property — their formatting is lost on read/write.

- **Title (`<h1>`)** must never be included in write operations. Notes manages it via
  `name`. `note_ops.py` strips it automatically.

- **Images** embedded in notes appear as `<img src="data:image/..."/>` — read-only,
  never generate base64 image tags.
