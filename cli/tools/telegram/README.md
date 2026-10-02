# telegram: operator guide

`telegram` is a command-line tool for the Telegram Bot API, built for AI agents. It follows the Agent
CLI Contract 1.4.2 (`contract/CONTRACT.md` at the repository root). Every call prints exactly one JSON
document. The bot token stays in the tool's own environment, and the agent never handles it.

What it does: reads a bot's incoming messages (`updates list`), sends text, documents and photos
(`messages send`, `send-document`, `send-photo`), and waits for a person's reply (`updates wait`), so
an agent can hand a decision to a human on their phone and carry on with the answer.

This guide is for the people who **install and run** the tool. Agents learn it from the tool itself:
`telegram teach`, `telegram tools`, `telegram describe <command>`.

- [Builds: full vs read-only](#builds-full-vs-read-only)
- [Building and installing](#building-and-installing)
- [Credentials and the profile-scoped token rule](#credentials-and-the-profile-scoped-token-rule)
- [Settings and the config file](#settings-and-the-config-file)
- [Profiles: one profile is one bot](#profiles-one-profile-is-one-bot)
- [The chat allowlist](#the-chat-allowlist)
- [How the update queue works](#how-the-update-queue-works)
- [Delegating to a human: send, then wait](#delegating-to-a-human-send-then-wait)
- [Writes and the upload risk](#writes-and-the-upload-risk)
- [MCP server mode](#mcp-server-mode)
- [Running in a container](#running-in-a-container)
- [Checking an installation](#checking-an-installation)
- [Not migrated yet](#not-migrated-yet)

## Builds: full vs read-only

| Binary | Build tags | Can send or ack | `--allow-any-chat` | MCP server |
|---|---|---|---|---|
| `bin/telegram` | `mcp` | yes, with `--confirm` | yes | yes |
| `bin/telegram-ro` | `mcp,readonly` | no; write code is not compiled in | no | yes |

**Use `telegram-ro` unless the agent really needs to send.** The read-only build does not hide the
write commands; it contains none of their code. An agent that asks for one gets `refused` with
`details.reason: "writes_disabled"`, and naming `--allow-any-chat` gets `refused` with
`details.reason: "any_chat_requires_write_build"`. Neither appears in `tools`, `describe`, `teach` or
the MCP tool list.

Choose read-only in particular when:

- the tool is served over MCP. Permission rules that an agent client applies to shell commands never
  see MCP calls, so the build itself is the only write protection that holds across both;
- the tool is reachable from another machine (`serve --transport http` on a non-loopback address);
- the host holds files that must not leave it. The `send-document` and `send-photo` commands upload
  any file the process can read (see [the upload risk](#writes-and-the-upload-risk)).

## Building and installing

Go 1.25 or later. Nothing is needed at runtime: binaries are static (`CGO_ENABLED=0`).

```sh
make                       # bin/telegram and bin/telegram-ro for this machine
make test                  # go vet + go test -race under every tag set: "", readonly, mcp, mcp,readonly
make verify                # contract conformance script against both binaries
make release               # dist/: both flavours for linux, darwin (amd64, arm64) and windows/amd64, plus SHA256SUMS-<version>
make build TAGS=readonly OUT=bin/telegram-ro-nomcp   # any other combination
make image                 # container image of the read-only build; IMAGE_TAGS=mcp for the full one
```

`VERSION` comes from this tool's own git tags, named `telegram/v<semver>`. Tags for other tools in
the repository are ignored, and the prefix is stripped, so `git tag telegram/v1.0.0` builds version
`1.0.0`. Commits after a tag build as `1.0.0-<n>-g<hash>`, uncommitted changes add `-dirty`, and with no
tag the commit hash is used. Override with `make release VERSION=1.2.3`. `telegram version` reports the
version, the commit, `writes_enabled` and `mcp_enabled`.

A build without the `mcp` tag contains no server code: `serve` answers `refused` with
`details.reason: "mcp_disabled"`.

Install by symlinking the built binaries onto your `PATH`, so a rebuild is picked up at once:

```sh
ln -s <repo>/tools/telegram/bin/telegram    ~/.local/bin/telegram
ln -s <repo>/tools/telegram/bin/telegram-ro ~/.local/bin/telegram-ro
```

## Credentials and the profile-scoped token rule

The only credential is the **bot token** from @BotFather (`BOT_TOKEN`). Telegram puts the token in the
URL path (`/bot<token>/<method>`), so the tool never prints that URL: errors, logs and previews show
`bot<redacted>`, and every output stream is redacted before it is written.

**Never put a token on the command line.** `--token`, `--key`, `--api-key`, `--password`, `--secret` and
`--credential` and `--bot-token` are refused before anything else is parsed, and the refusal tells you
to rotate the token (revoke it with @BotFather `/revoke`). There is no token flag at all.

Credential files (including `env_file`) must be mode **0600 or 0400**. Anything looser is a `config`
error. The config file holds **paths only**: a config file containing a token value is refused.

The tool has two scopes for the token:

- **Default scope**: the unsuffixed `TELEGRAM_BOT_TOKEN` and `TELEGRAM_BOT_TOKEN_FILE`, and the config
  file's `default` section. This is the scope that runs when no profile is selected.
- **Profile scope**: `TELEGRAM_BOT_TOKEN_<PROFILE>`, `TELEGRAM_BOT_TOKEN_FILE_<PROFILE>`, and the
  selected profile's own `bot_token_file` or `env_file` in the config file.

**A selected profile resolves its bot token only from the profile scope, never from the default
scope.** The default scope is skipped entirely for a profile, even when it is the only place a token
exists. Settings such as `default_chat` still inherit, but the credential does not. The reason is
safety: with one profile per bot, falling back to the default bot's token would silently send as the
wrong bot after a typo or a missing `bot_token_file`.

What a selected profile does, in order:

1. `TELEGRAM_BOT_TOKEN_<PROFILE>`
2. `TELEGRAM_BOT_TOKEN_FILE_<PROFILE>`
3. the profile's `bot_token_file`, or its `env_file` holding the key
4. an `env_file` in the `default` section holding the `TELEGRAM_BOT_TOKEN_<PROFILE>` key (the key names
   the profile, so this is the one default-section source that counts as profile scope)

A profile with its own source uses it even when `TELEGRAM_BOT_TOKEN_FILE` is exported. A profile with
no source of its own, where only the default scope has a token, is a `config` error (exit 3), whose
`details` name the profile and the skipped source (a variable name or file pointer, never a value).
With no token anywhere it is the ordinary `auth` error. This is checked before any request, so
`--dry-run` fails the same way.

With no profile selected, the order is the contract's full §12.1 order and the `default` bot is used.
`telegram list-config` shows the source in effect for `BOT_TOKEN`, and for a profile that would have
used a default-scope source it shows `set: false` with a hint.

Recommended setup on a workstation:

```sh
mkdir -p ~/.secrets && chmod 700 ~/.secrets
install -m 600 /dev/null ~/.secrets/telegram-<bot>.token
$EDITOR ~/.secrets/telegram-<bot>.token       # the token, alone on one line
```

and in the config file, `"bot_token_file": "~/.secrets/telegram-<bot>.token"`. Then confirm it works:
`telegram doctor`.

## Settings and the config file

`telegram teach config` is the authoritative reference: every setting, its environment variable, flag
and config-file member, the precedence rules, platform paths, and a worked example. `telegram
list-config` shows the value in effect for each setting and where it came from, and
`telegram list-config --schema` prints the config file's JSON Schema.

Settings specific to this tool, on top of the contract's common ones (`LIMIT`, `MAX_BYTES`, `TIMEOUT`,
`BUDGET`, `LOG_LEVEL`, ...):

| Setting | Default | Notes |
|---|---|---|
| `BOT_TOKEN` | none | credential, [profile-scoped](#credentials-and-the-profile-scoped-token-rule) |
| `DEFAULT_CHAT` | none | the chat used when `--chat` is absent; always implicitly allowed |
| `ALLOWED_CHATS` | none | comma-separated chat ids this profile may address without `--allow-any-chat` |
| `PARSE_MODE` | `none` | `none`, `html` or `markdownv2`; `--parse-mode` overrides per send |
| `POLL_INTERVAL` | 2s | pause between polls of a wait while the queue is non-empty (minimum 500ms) |
| `BASE_URL` | `https://api.telegram.org` | overrides are honoured **only** for loopback hosts (test doubles) |
| `DATASET_*` | per contract | present because the contract requires them; this tool has no dataset commands |

Resolution order for every setting: flag, `TELEGRAM_<SETTING>_<PROFILE>`, `TELEGRAM_<SETTING>`, the
config file's profile, the config file's `default`, `AGENTCLI_<SETTING>`, then the built-in default.
Durations and sizes need units (`30s`, `4h`).

The config file is `<config base>/agentcli/telegram/config.json`:

| Platform | Config base |
|---|---|
| Linux and other Unix | `$XDG_CONFIG_HOME`, else `~/.config` |
| macOS | `$XDG_CONFIG_HOME` if set, else `~/Library/Application Support` |
| Windows | `%APPDATA%` |

With no home directory (containers, service accounts), set `TELEGRAM_CONFIG`; the tool refuses to
guess.

## Profiles: one profile is one bot

A profile is a named, **partial** set of overrides, and for this tool **one profile is one bot**: its
token plus its chat settings. The `default` section is the bot that runs with no `--profile`. Other
bots are named profiles.

```json
{
  "config_version": 1,
  "default": {
    "description": "General assistant bot. Messages the owner.",
    "bot_token_file": "~/.secrets/telegram-assistant.token",
    "default_chat": "<owner-chat-id>",
    "allowed_chats": "<owner-chat-id>,<partner-chat-id>"
  },
  "profiles": {
    "builds": {
      "description": "CI bot. Posts build and long-running task results to the owner.",
      "bot_token_file": "~/.secrets/telegram-builds.token",
      "parse_mode": "html"
    },
    "family": {
      "description": "Family bot. Shared household notifications to the family group.",
      "bot_token_file": "~/.secrets/telegram-family.token",
      "default_chat": "<family-group-chat-id>",
      "allowed_chats": "<family-group-chat-id>"
    }
  }
}
```

**Do not commit a config file that contains real chat ids.** Chat ids are personal identifiers (a
private chat's id is the person's Telegram user id). They are not credentials, so the tool accepts
them in the config file, but keep such a file out of version control and use placeholders in anything
shared.

Notes:

- `description` is optional: one line, 1 to 200 characters, in `default` and in each profile.
  `telegram list-profiles` shows it so an agent can choose a bot without guessing. It is not a
  setting: no variable, no flag, not in `list-config`.
- `builds` above has its own `bot_token_file` (required) and **inherits** `default_chat` and
  `allowed_chats` from `default`, as every setting does. That is intended, but it means `builds` will
  message the owner. A private chat id works across bots, but only once that person has pressed Start on
  *each* bot.
- `--profile builds` (or `TELEGRAM_PROFILE=builds`) selects it. A profile can also exist only in the
  environment (`TELEGRAM_BOT_TOKEN_FILE_BUILDS=...`). In variable names a profile is uppercased, with
  `-` becoming `_`. A profile may not be named `default` or `FILE`, and an unknown profile is a
  `config` error with a `list-profiles` hint.
- `list-profiles` lists names and descriptions only, never values.

## The chat allowlist

A chat-scoped command addresses `--chat <id>` if given, else `DEFAULT_CHAT`. The profile's **allowed
set** is `DEFAULT_CHAT` plus `ALLOWED_CHATS`. A chat outside it is `refused` with
`details.reason: "chat_not_allowed"`, and the hint is the same command line with `--allow-any-chat`
(full build) or "add it to allowed_chats" (read-only build).

- **`--allow-any-chat`** lifts the check for that call. It exists only in the full build. It is also how
  you discover a chat id: `telegram updates list --allow-any-chat --fields update_id,chat.id,chat.type,from.username`
  after someone has messaged the bot.
- For `updates list` and `updates wait`, the allowlist is a **filter**: updates from chats outside it are
  dropped and counted in a `filtered_chats` warning (counts only, never ids). An empty allowed set
  filters everything and warns `no_allowed_chats`.
- Chat ids are strings: `-1001234567890` (negative for groups) or `@channelname` for public channels.
- A bot can only message a user who has opened it and pressed **Start**. A 403 from Telegram is exit
  code 32, `chat_unreachable`: the bot was blocked, never started, or kicked.

## How the update queue works

A bot cannot read chat history. It sees a queue of **updates** that Telegram holds for it. The tool
treats that queue carefully, because the Bot API gives it destructive edges:

- **100-update window.** `getUpdates` shows at most the oldest 100 unconfirmed updates. When 100 come
  back, newer ones are invisible until something acknowledges the old ones. The tool reports this as a
  `queue_window_full` warning.
- **24-hour expiry.** Telegram drops unconfirmed updates after 24 hours.
- **One consumer.** Only one `getUpdates` reader per bot may run at a time, and a webhook set on the bot
  blocks `getUpdates` altogether. A second reader gets `conflict` (exit 7): check for another poller
  and for a webhook.
- **Acknowledging is destructive.** An `offset` confirms, and permanently deletes, every update below
  it **for every consumer of that bot**. So `updates list` and `updates wait` never send an offset (nor
  `allowed_updates`, which Telegram stores server-side) and filter locally. Only `updates ack --through
  <update_id> --confirm` does, and it is marked destructive. The full build alone has it.
- There is no "get all messages": history older than the queue is not available through the Bot API.

## Delegating to a human: send, then wait

The flow an agent uses to ask a person something and continue with the answer:

```sh
telegram messages send --text "Deploy is ready. Reply to approve." --confirm
# -> data.message_id: 4021
telegram updates wait --after-message 4021 --max-wait 5m
# -> the first matching message, or ok:true, data:[] and a "no_reply" warning
```

`--reply-to <id>` matches only a Telegram reply to that message, `--after-message <id>` matches any
newer message in the chat, and giving neither means "anything newer than what was already queued".
`--max-wait` defaults to 60s (maximum 1h). The call's default budget is `--max-wait` plus 30s; setting
`--budget` below `--max-wait` is a `usage` error. No reply in time is a success with an empty list,
not an error. Interrupting a wait (SIGINT) ends it as `canceled`.

While the queue is empty the wait long-polls (up to 25s per request); while unrelated updates are
pending it sleeps `POLL_INTERVAL` between polls, never busy-looping. If 100 updates are pending and
none match, it stops with `queue_window_full` instead of waiting blind.

## Writes and the upload risk

The full build adds `messages send`, `messages send-document`, `messages send-photo` and `updates ack`.
Every one:

- is refused without `--confirm`, and the refusal shows the exact request as a preview (token
  redacted), with nothing sent;
- sends nothing with `--dry-run`, even alongside `--confirm`;
- is never retried automatically. A timeout reports `details.write_state: "unknown"` and a 429 reports
  `rate_limited` with `retry_after_ms`, because Telegram has no idempotency keys and a retry could
  send twice.

`--text-file <path>` (instead of `--text`) keeps long text out of argv and shell history; the file
must be a regular UTF-8 file of at most 1 MiB.

**`send-document` and `send-photo` upload any local file the process can read** to a chat. Anyone in
that chat can download it, and with `--allow-any-chat` the chat can be any chat the bot can reach. The
confirmation preview shows the resolved path, the name and the size, never the content. That helps a
careful agent, but it does not protect you from a careless or manipulated one (a prompt-injected agent
can be told to send `~/.ssh/id_ed25519` somewhere). If the host has files that must not leave it, do
one of these:

- **run `telegram-ro`**, which has no upload code at all (recommended);
- run the full build as a user that can read only what it should, or in a container with nothing
  sensitive mounted;
- keep each profile's allowlist to chats you control, and avoid `--allow-any-chat` in agent
  permission rules.

Telegram's limits: documents up to 50 MB, photos up to 10 MB, text up to 4096 characters, captions up
to 1024. Over-limit requests come back as `validation`; the tool does not guess locally.

## MCP server mode

`telegram serve` runs the same binary as a Model Context Protocol server. Each command becomes a tool
with the same name, parameters and JSON envelope. `tools`, `describe` and `list-profiles` are left out
(the protocol lists tools itself, and a server's profile is fixed when it starts). `teach` is offered.
Per-call flags become arguments with underscores (`--max-bytes` → `max_bytes`), and write tools take
`confirm` and `dry_run`. A read-only build lists no write tools and no `allow_any_chat` argument.

`serve` accepts only server flags: `--transport`, `--addr`, `--allow-remote`, `--token-file`,
`--tls-cert-file`, `--tls-key-file`, `--tls-terminated-upstream`, `--profile`, `--config`, `--verbose`,
`--deterministic`. The profile, config and determinism you start it with apply to every call. The bot
token is resolved per call, so `initialize` and the tool list work before any token is configured.

Timeouts and budgets apply per call. `updates wait` over MCP honours `max_wait` per call, and ends
promptly when the client cancels the call or the server shuts down.

### stdio (local agents)

```sh
telegram-ro serve
```

The client starts the process and talks over stdin/stdout. Nothing but protocol messages is written to
stdout; the startup and shutdown lines go to stderr. Example for Claude Code:

```sh
claude mcp add telegram -e TELEGRAM_CONFIG=$HOME/.config/agentcli/telegram/config.json -- ~/.local/bin/telegram-ro serve
```

### Streamable HTTP

```sh
telegram-ro serve --transport http                       # http://127.0.0.1:7810
telegram-ro serve --transport http --addr 9000           # a bare port still binds loopback
```

The MCP endpoint is any path except `/health`; clients normally use `http://127.0.0.1:7810/mcp`.

#### What every HTTP listener enforces

| Request | Response |
|---|---|
| `GET /health` | `200`, unauthenticated: `tool`, `tool_version`, `contract_version`, `writes_enabled`, `mcp_enabled`. Nothing else |
| `Origin` header present and not loopback | `403` (blocks web pages reaching a local server through DNS rebinding). No `Origin` is allowed |
| Body over 1 MiB | `413` |
| Token configured and missing or wrong | `401`, no detail. Compared in constant time |
| New session when 64 are open | `503`. Sessions idle for 30 minutes are closed |

**Not offered over HTTP**, because they act on the server's own filesystem or configuration, which a
remote client does not share: `doctor` and `list-config`. This tool has no dataset commands, so none
of the `dataset.*` tools exist on either transport.

#### The bearer token

Optional on loopback, **required** on any other address. Sources, first match wins:

1. `TELEGRAM_MCP_TOKEN`: the value
2. `TELEGRAM_MCP_TOKEN_FILE`: a path
3. `--token-file <path>`

Never pass the token itself as an argument. Token files must be mode 0600 or 0400. This is the MCP
server's own token, unrelated to the Telegram bot token.

```sh
openssl rand -hex 32 > ~/.secrets/telegram-mcp.token && chmod 600 ~/.secrets/telegram-mcp.token
telegram-ro serve --transport http --token-file ~/.secrets/telegram-mcp.token
```

Clients send `Authorization: Bearer <token>`:

```sh
claude mcp add --transport http telegram http://127.0.0.1:7810/mcp --header "Authorization: Bearer $(cat ~/.secrets/telegram-mcp.token)"
```

#### Remote access: the three requirements

The server will not start on a non-loopback address (`0.0.0.0`, `::`, a LAN IP, or any hostname other
than `localhost`) unless it has all three of these. Otherwise it reports a `config` error before
listening:

1. `--allow-remote`
2. a token (above)
3. a TLS decision, exactly one of:
   - `--tls-cert-file <path> --tls-key-file <path>`: the tool serves HTTPS itself (TLS 1.2+), or
   - `--tls-terminated-upstream`: you declare that a reverse proxy, ingress, service mesh or
     encrypted overlay terminates TLS in front of the tool, and the hop to it is private.

Giving both a certificate and `--tls-terminated-upstream` is a `usage` error. The same choices can come
from the environment: `TELEGRAM_MCP_TLS_CERT_FILE`, `TELEGRAM_MCP_TLS_KEY_FILE`, and
`TELEGRAM_MCP_TLS_TERMINATED_UPSTREAM=true`.

Why plaintext is refused by default: a bearer token over plain HTTP can be read by anything on the
network path and replayed. Behind that token sits a bot that can message people.

When writes are enabled on a non-loopback address, the server prints a warning recommending the
read-only build. Take the advice.

#### Option A: the tool serves HTTPS, with a certificate from mkcert

[mkcert](https://github.com/FiloSottile/mkcert) creates a private certificate authority and
certificates it signs. It suits a home lab or a small team, without a public domain.

On the **server**:

```sh
mkcert -install                                   # creates the local CA (once)
mkdir -p /etc/telegram/tls && cd /etc/telegram/tls
mkcert -cert-file cert.pem -key-file key.pem telegram.lan 192.168.1.20 localhost 127.0.0.1
chmod 600 key.pem                                 # required: a looser key file is refused
mkcert -CAROOT                                    # prints where rootCA.pem lives
```

List every name and IP clients will use, because the certificate is checked against them. At startup
the tool loads the pair, checks that the key matches the certificate and that the certificate is
currently valid, and refuses to start otherwise.

```sh
TELEGRAM_MCP_TOKEN_FILE=/etc/telegram/mcp.token \
  telegram-ro serve --transport http --addr 0.0.0.0:7810 --allow-remote \
    --tls-cert-file /etc/telegram/tls/cert.pem --tls-key-file /etc/telegram/tls/key.pem
```

On each **client** machine, trust the CA: copy `rootCA.pem` (never `rootCA-key.pem`) from the server's
`mkcert -CAROOT` directory and either run `CAROOT=<dir> mkcert -install` there, or add it to the
client runtime's trust store. For Node-based clients: `NODE_EXTRA_CA_CERTS=/path/rootCA.pem`. Then:

```sh
curl --cacert rootCA.pem https://telegram.lan:7810/health
```

Installing the CA is the fix for a trust error, never disabling verification. Certificates from a
public CA (Let's Encrypt) or your organisation's CA need no client-side trust step.

#### Option B: TLS terminated in front of the tool

Behind nginx, Caddy, Traefik, a Kubernetes ingress, a service mesh with mTLS, or an encrypted overlay
such as Tailscale or WireGuard:

```sh
TELEGRAM_MCP_TOKEN_FILE=/etc/telegram/mcp.token \
  telegram-ro serve --transport http --addr 0.0.0.0:7810 --allow-remote --tls-terminated-upstream
```

The tool serves **plaintext**, and its startup line says so:
`"tls":"plaintext; TLS terminated upstream (operator-declared)"`. The tool cannot check your claim.
Make sure the listener is reachable only from the proxy (bind to the private interface, a Kubernetes
`ClusterIP`, or a firewall rule). The bearer token is still required, and the proxy should pass the
`Authorization` header through unchanged.

#### Startup and shutdown lines

On stderr, one JSON line each, containing no secrets:

```json
{"level":"info","msg":"serve started","transport":"http","addr":"127.0.0.1:7810","writes_enabled":false,"auth":"on","tls":"plaintext on loopback"}
```

`tls` is one of `https`, `plaintext on loopback`, or
`plaintext; TLS terminated upstream (operator-declared)`. SIGINT or SIGTERM shuts the server down
gracefully, cancelling any call still running; the exit status is 130 or 143.

## Running in a container

The `Dockerfile` builds the **read-only** flavour by default (`--build-arg TAGS=mcp` for the full one)
onto `gcr.io/distroless/static-debian12:nonroot`: the binary only, running as a non-root user with no
shell. That user has no home directory, so the image sets `TELEGRAM_CONFIG=/config/config.json` and
`TELEGRAM_DATASET_DIR=/data/datasets`. Mount the config read-only, and `/data` writable if you want
the dataset directory to exist (this tool stores nothing there beyond `doctor`'s probe):

```sh
docker run --rm \
  -v "$PWD/config.json:/config/config.json:ro" \
  -v "$HOME/.secrets/telegram-<bot>.token:/secrets/telegram-<bot>.token:ro" \
  -v telegram-data:/data \
  telegram-ro updates list
```

with `"bot_token_file": "/secrets/telegram-<bot>.token"` in the config. A mounted token file must still
be 0600 or 0400, and readable by the container's user (uid 65532).

To serve MCP from a container, remember that `0.0.0.0` inside the container counts as remote, so all
three requirements apply:

```sh
docker run --rm -p 127.0.0.1:7810:7810 \
  -v "$PWD/config.json:/config/config.json:ro" -v "$HOME/.secrets:/secrets:ro" -v telegram-data:/data \
  -e TELEGRAM_MCP_TOKEN_FILE=/secrets/telegram-mcp.token \
  telegram-ro serve --transport http --addr 0.0.0.0:7810 --allow-remote --tls-terminated-upstream
```

This is acceptable when the published port binds to the host's loopback, as above, or sits behind a
TLS proxy. Otherwise mount a certificate and use `--tls-cert-file`/`--tls-key-file`.

## Checking an installation

`doctor` is the smoke test. It checks the config file, the credential, the network, authentication
(`getMe`, reported as "Authenticated as bot @name"), clock skew and the dataset directory:

```sh
telegram version          # flavour: writes_enabled, mcp_enabled
telegram doctor           # run it per profile: telegram --profile builds doctor
telegram list-profiles    # names and descriptions
telegram list-config      # every setting and its source
telegram teach            # what an agent sees first
```

`doctor` exits non-zero when something an agent would hit is wrong, and says what to do about it. For
each profile, confirm the bot username it reports is the bot you expect.

## Not migrated yet

The `pr-agent` and `nintendo-stock-watcher` projects still talk to Telegram their own way and have not
been moved to this tool. `pr-agent`'s hooks block Bash commands that mention `api.telegram.org`,
`telegram-bot-token` or `.config/pr-agent`; the tool keeps those out of argv, but adopting it there is
a separate decision.
