# Go Pattern

The Go realization of [`../BASE_TEMPLATE.md`](../BASE_TEMPLATE.md). Section numbers in brackets refer
to that file. Go is the default language for this family; the reasoning is in
[`../../AGENT.md`](../../AGENT.md).

SDK signatures below were checked against the SDK source. Re-check them before relying on one.

## 1. Layout

```
<tool>/
  main.go                     # dispatch and os.Exit, nothing else
  internal/registry/          # commands, schema derivation, startup validation
  internal/config/            # resolver, profiles, platform paths
  internal/secrets/           # credential resolution, permission checks, redactor
  internal/upstream/          # client, retries, pagination adapter
  internal/shape/             # --fields, empty-stripping, caps, ordering
  internal/envelope/          # assembly, the single write, exit mapping
  internal/datasets/          # storage, sidecars, cleanup, dataset commands
  internal/render/            # --human
  internal/teach/             # orientation.md.tmpl, shared.md.tmpl (vendored), domain topics
  internal/diagnostics/       # doctor, list-config, list-profiles
  internal/serve/             # MCP; build tag `mcp`
  internal/conformance/       # the checklist, as tests
```

Everything under `internal/`, so no second consumer can appear and constrain the design.

## 2. Toolchain and dependencies

Go 1.25 or later (`os.Root`, used to confine dataset I/O).

| Module | Purpose | Notes |
|---|---|---|
| `github.com/modelcontextprotocol/go-sdk/mcp` | Server mode | v1.8.0 at time of writing. Only linked in builds with the `mcp` tag |
| `github.com/google/jsonschema-go` | Schema derivation and validation | Already pulled in by the SDK. Supports JSON Schema 2020-12 |
| `github.com/spf13/cobra` | Command tree | Optional. Stdlib `flag` plus a dispatch map is a fine zero-dependency alternative, since the tree is built from the registry either way |

Everything else from the standard library. Confirm before adding anything.

## 3. Registry [§2]

Hold types, not schema JSON. `jsonschema-go` derives both schemas from the input and output types,
once, at registration, and `describe` and the MCP server publish the same result. The struct is the
only source:

```go
type CardsListInput struct {
    Board  string `json:"board" jsonschema:"Board id"`
    ListID string `json:"list_id,omitempty" jsonschema:"Only cards in this list"` // --list is reserved
}
```

Open the derived output schema before storing it (contract §6.2; base template §2): `ForType` makes
every field without `omitempty` required and closes every object.

Generic entries do not share a type, so the registry stores an interface, and each entry is a closure
that captures its concrete types and exposes `Definition()`, `RunCLI(args)`, and `Register(*mcp.Server)`
(the last only in `mcp` builds).

Read-only builds exclude writes at compile time: put the mutating registrations in a file with
`//go:build !readonly`. The commands are then absent from the binary, not merely hidden. Keep a plain
`[]string` of their names in an untagged file, so the read-only build can still refuse them with
`writes_disabled`.

## 4. Envelope [§4]

```go
type Envelope struct {
    OK      bool            `json:"ok"`
    Tool    string          `json:"tool"`
    Command *string         `json:"command"`
    Data    json.RawMessage `json:"data"`
    Error   *Error          `json:"error,omitempty"`
    Meta    Meta            `json:"meta"`
}
```

- `Command` is a pointer and `Data` has no `omitempty`: both must appear, as `null` where applicable.
- `Data` is `json.RawMessage` because it is built by the shaping layer as ordered JSON (§6 below), not
  by marshalling a map.
- In `meta.page`, no member carries `omitempty`: the contract requires every member every time, and
  `omitempty` on `has_more` would silently drop `false`.

Write the whole document at once:

```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false) // keep & < > readable in URLs
if pretty { enc.SetIndent("", "  ") }
_ = enc.Encode(env)
out := redactor.Apply(buf.Bytes())
if _, err := os.Stdout.Write(out); err != nil { os.Exit(1) }
```

One `Write` call on the finished buffer satisfies the "single write" rule. `Encode` appends the
trailing newline. Into a pipe, a document larger than the pipe buffer (64 KiB) can still be cut off
by SIGKILL while the write is blocked; contract §2 states this limit. Do not try to work around it:
the caught signals (SIGINT, SIGTERM) already let the write finish.

Recover panics in `main` and emit an `internal` envelope. Catch `SIGINT` and `SIGTERM` with
`signal.NotifyContext`, cancel the context, and exit 130 or 143 with a `canceled` envelope.

## 5. Configuration [§5]

```go
cfgBase, err := os.UserConfigDir() // XDG_CONFIG_HOME / ~/.config, ~/Library/Application Support, %APPDATA%
cacheBase, err := os.UserCacheDir() // XDG_CACHE_HOME / ~/.cache, ~/Library/Caches, %LOCALAPPDATA%
```

These match the contract's platform table, including honouring XDG variables on macOS only when set.
Check that an XDG value is absolute before trusting it. On error, fail with `config`; do not
substitute `os.TempDir()`.

```go
type Setting struct {
    Name    string
    Value   any
    Source  string // "--timeout", "TRELLO_LIMIT", "config.json#default.limit", "builtin"
    Origin  string // flag | env_profile | env_default | file_profile | file_default | family | builtin
    Default any
    Secret  bool
}
```

Give `Setting` a `MarshalJSON` that emits `"value":null` and `"set":bool` when `Secret` is true. With
the redaction in the type, a new consumer cannot forget it.

With cobra, `cmd.Flags().Changed(name)` tells "set to its default value" apart from "not set", which is
the difference between `origin:"flag"` and a lower tier.

Load the config file with `json.Decoder.DisallowUnknownFields()` on the outer struct. Profile and
default sections are `map[string]json.RawMessage`, so check their keys against the known settings
yourself. Reject a credential member that is not a `_file` reference during the load.

Durations: `time.ParseDuration`. Sizes need a small parser for `KB MB GB KiB MiB GiB`; reject bare
numbers.

## 6. Shaping [§8, §11]

`encoding/json` marshals maps in sorted key order, which is stable but not the declared or requested
order. Build output as an ordered structure instead:

```go
type Field struct{ Key string; Value json.RawMessage }
type Record []Field // marshalled by hand, in order
```

Decode upstream responses with `json.Decoder.UseNumber()`, or keep values as `json.RawMessage`, so
large integers and decimals pass through unchanged.

String caps count runes (`utf8.RuneCountInString`), not bytes.

For the byte cap, marshal once, measure, and binary-search the number of records that fit. When the
answer is zero, keep the first record and shorten its strings (contract §8.4); never return an empty
page while records remain.

## 7. Secrets [§6]

```go
type Secret struct{ v string }
func (Secret) String() string               { return "[secret]" }
func (Secret) GoString() string             { return "[secret]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[secret]"`), nil }
func (s Secret) Reveal() string             { return s.v } // one call site: the auth header
```

A stray `fmt.Println` or `log` call then prints a placeholder.

Resolution, per credential, direct before file within each scope. A file that is named but unreadable
or too permissive stops resolution with `config`; falling through to a lower tier would hide the
mistake:

```go
func resolve(prefix, name, profile string) (Secret, string, error) {
    scopes := []string{}
    if profile != "" { scopes = append(scopes, "_"+normalize(profile)) }
    scopes = append(scopes, "")
    for _, sfx := range scopes {
        if v := os.Getenv(prefix + "_" + name + sfx); v != "" {
            return Secret{v}, prefix + "_" + name + sfx, nil
        }
        if p := os.Getenv(prefix + "_" + name + "_FILE" + sfx); p != "" {
            v, err := readProtected(p) // permission check, then read, then trim
            if err != nil { return Secret{}, "", configError(err) } // named but unusable: never fall through
            return Secret{v}, p, nil
        }
    }
    return Secret{}, "", nil // not found here; try the config file's profile and default sections
}
```

`readProtected` checks `info.Mode().Perm()&0o077 == 0` on Unix. On Windows `Mode()` does not reflect
ACLs, so skip the check there and add the `permissions_unverified` warning.

Scan `os.Args` for forbidden flags before handing it to the parser, matching `--api-key`,
`--api-key=…`, and the other names, so the refusal happens even if the parser would have errored
first.

Redactor: build one `strings.Replacer` at startup from each secret's raw, `url.QueryEscape`,
`base64.StdEncoding`, and `base64.URLEncoding` forms, the full header value, and every prefix of 12 or
more characters. Order the pairs longest first. Apply it to the serialized output bytes, and wrap
`os.Stderr` and the dataset writer with an `io.Writer` that applies it too.

## 8. Upstream [§7]

- An `http.Client` with an explicit `Timeout`, plus a per-request context deadline. Never
  `http.DefaultClient`: it has no timeout, so the `timeout` code would never fire.
- `CheckRedirect` refuses a different host and stops after 3 hops.
- Drain and close non-2xx bodies through `io.LimitReader(body, 4096)`, so connections are reused
  without reading an unbounded error body.
- `User-Agent: <tool>/<version> agentcli/<contract> (<GOOS>/<GOARCH>)`.

## 9. Datasets [§10]

Open the dataset directory once with `os.OpenRoot` and do every file operation through it. A
malformed dataset id then cannot escape the directory.

```go
root, err := os.OpenRoot(dir)
f, err := root.OpenFile(id+".jsonl.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
// write, f.Sync(), f.Close(), then root.Rename(id+".jsonl.tmp", id+".jsonl")
```

Create the directory with `os.MkdirAll(dir, 0o700)` followed by `os.Chmod(dir, 0o700)`, since
`MkdirAll` applies the umask.

Ids: UUIDv4 from `crypto/rand`, about ten lines, no dependency. Under `--deterministic`, the first 16
bytes of a SHA-256 over the canonical request instead.

Reading: seek to the nearest indexed offset, then `bufio.Scanner` forward, with a raised buffer
(`sc.Buffer(make([]byte, 0, 1<<20), 16<<20)`), since the default 64 KB fails on long records. Emit each
line as `json.RawMessage`; it is already in final shape.

Cleanup lock: `root.OpenFile(".cleanup.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)`. On
`os.IsExist`, break the lock if its modification time is older than 60 seconds. Avoid `flock`; it
behaves differently across the filesystems a container may mount.

Take a `now func() time.Time` in the dataset store so tests can drive expiry without sleeping.

## 10. Teach [§14]

```go
//go:embed orientation.md.tmpl
var orientation string

//go:embed shared.md.tmpl
var shared string

//go:embed topics/*.md
var topics embed.FS
```

Render with `text/template`. The drift test hashes the embedded `shared` and `orientation` and
compares them with the bundle's copies, failing with the command to re-vendor.

## 11. MCP server [§16]

Verified against SDK v1.8.0 source:

```go
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])

type ToolHandlerFor[In, Out any] func(ctx context.Context, req *CallToolRequest, input In) (*CallToolResult, Out, error)

func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler
```

- `Tool` has `Name`, `Title`, `Description`, `InputSchema any`, `OutputSchema any`,
  `Annotations *ToolAnnotations`, and `Icons`. The output schema is native; no extension is needed.
- `ToolAnnotations` has `ReadOnlyHint bool`, `IdempotentHint bool`, `DestructiveHint *bool`,
  `OpenWorldHint *bool`, and `Title`. The pointer fields distinguish unset from `false`; set them
  deliberately.
- `CallToolResult` has `Content []Content`, `StructuredContent any`, and `IsError bool`. Put the
  envelope in `StructuredContent` and the same envelope, serialized, in one `*mcp.TextContent`.
- Do not let the generic `AddTool` derive `OutputSchema` from `Out`. It derives a strict schema:
  `jsonschema-go` marks every field without `omitempty` as required and sets
  `additionalProperties:false`, which contract §6.2 forbids. Use `(*Server).AddTool(tool, handler)`
  instead, with the `inputSchema` from the registry and an `OutputSchema` built as follows. Start from
  the envelope schema, then replace `data` with `{"anyOf":[<open outputSchema>,{"type":"null"}]}`.
  The raw handler receives `req.Params.Arguments` as `json.RawMessage`. Bind it onto the same input
  argv parsing produces. Validating the arguments and the result is then the caller's job, so let
  the substrate do it.

```go
srv := mcp.NewServer(&mcp.Implementation{Name: tool, Version: version}, nil)
registry.RegisterAll(srv)

switch transport {
case "stdio":
    err = srv.Run(ctx, &mcp.StdioTransport{})
case "http":
    h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
    hs := &http.Server{
        Addr:              addr,
        Handler:           harden(h), // origin check, body cap, auth, /health
        ReadHeaderTimeout: 10 * time.Second,
        TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
    }
    if certFile != "" {
        err = hs.ListenAndServeTLS(certFile, keyFile) // key file permission-checked first
    } else {
        err = hs.ListenAndServe() // loopback, or remote with --tls-terminated-upstream only
    }
}
```

Refuse to reach this point for a remote address without a TLS decision; the check belongs with the
other bind checks, before anything listens. Load the pair once with `tls.LoadX509KeyPair` during
startup validation, so a mismatched or unreadable certificate fails with `config` before the server
starts rather than on the first handshake.

`harden` wraps the handler with, in order: `/health` short-circuit, `Origin` check,
`http.MaxBytesReader` at 1 MiB, and a bearer check using `subtle.ConstantTimeCompare`.

In stdio mode, stdout belongs to the protocol. Route all logging to stderr and test that nothing else
writes to stdout: a stray print corrupts the session and surfaces as a confusing client-side parse
error.

Put all of `internal/serve` behind `//go:build mcp`. Builds without the tag get a stub `serve` that
returns `refused` with `mcp_disabled`, and link none of the SDK.

## 12. Build [§18]

```make
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
TAGS    ?=              # add: mcp, readonly

build:
	CGO_ENABLED=0 go build -trimpath -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o bin/$(TOOL) .
```

`CGO_ENABLED=0` yields a truly static binary that runs on an empty base image. `-trimpath` keeps local
paths out of the binary. Cross-compile by setting `GOOS` and `GOARCH`.

```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot
COPY bin/<tool> /<tool>
ENV <TOOL>_CONFIG=/config/config.json <TOOL>_DATASET_DIR=/data/datasets
VOLUME /data
ENTRYPOINT ["/<tool>"]
```

The `nonroot` user has no home directory, so set both paths explicitly; the tool will not guess.

## 13. Tests [§19]

- `net/http/httptest` for a fake upstream that counts requests by method.
- Golden envelopes under `--deterministic`, compared as bytes.
- `t.TempDir()` per test, so dataset tests can run in parallel.
- Run dataset concurrency tests with `-race`.
- Inject the clock rather than sleeping.
