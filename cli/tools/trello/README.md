# trello: operator guide

`trello` is a command-line tool for the Trello REST API, built for AI agents. It follows the Agent CLI
Contract 1.4 (`contract/CONTRACT.md` at the repository root). Every call prints exactly one JSON
document. Credentials stay in the tool's own environment, and the agent never handles them.

This guide is for the people who **install and run** the tool. Agents learn it from the tool itself:
`trello teach`, `trello tools`, `trello describe <command>`.

- [Builds: full vs read-only](#builds-full-vs-read-only)
- [Building](#building)
- [Credentials](#credentials)
- [Settings and the config file](#settings-and-the-config-file)
- [Profiles](#profiles)
- [Datasets](#datasets)
- [Writes and the attach-file risk](#writes-and-the-attach-file-risk)
- [MCP server mode](#mcp-server-mode)
- [Running in a container](#running-in-a-container)
- [Checking an installation](#checking-an-installation)

## Builds: full vs read-only

| Binary | Build tags | Can change Trello | MCP server |
|---|---|---|---|
| `bin/trello` | `mcp` | yes, with `--confirm` | yes |
| `bin/trello-ro` | `mcp,readonly` | no; write code is not compiled in | yes |

**Use `trello-ro` unless the agent really needs to make changes.** The read-only build does not hide
the write commands; it contains none of their code. If an agent asks for one, it gets
`refused` with `details.reason: "writes_disabled"`. The write commands don't appear in `tools`,
`describe`, `teach`, or the MCP tool list.

Choose read-only in particular when:

- the tool is served over MCP. Permission rules that an agent client applies to shell commands never
  see MCP calls, so the build itself is the only write protection that holds across both;
- the tool is reachable from another machine (`serve --transport http` on a non-loopback address);
- the host holds files that must not leave it. `cards attach-file` can upload any file the process
  can read (see [below](#writes-and-the-attach-file-risk)).

The Trello token's own scope is a second line of defence. A read-only token (`scope=read`) refuses
writes upstream whichever build you run.

## Building

Go 1.25 or later. Nothing is needed at runtime: binaries are static (`CGO_ENABLED=0`).

```sh
make                       # bin/trello and bin/trello-ro for this machine
make test                  # go vet + go test -race under every tag set: "", readonly, mcp, mcp,readonly
make verify                # contract conformance script against both binaries
make release               # dist/: both flavours for linux, darwin (amd64, arm64) and windows/amd64, plus SHA256SUMS-<version>
make build TAGS=readonly OUT=bin/trello-ro-nomcp   # any other combination
make image                 # container image of the read-only build; IMAGE_TAGS=mcp for the full one
```

`VERSION` comes from this tool's own git tags, named `trello/v<semver>`. Tags for other tools in the
repository are ignored, and the prefix is stripped, so `git tag trello/v1.0.0` builds version
`1.0.0`. Commits after a tag build as `1.0.0-<n>-g<hash>`, uncommitted changes add `-dirty`, and with
no tag the commit hash is used. `COMMIT` comes from `git rev-parse`. Override either with
`make release VERSION=1.2.3`. `trello version` reports both, plus `writes_enabled` and `mcp_enabled`.

A build without the `mcp` tag contains no server code: `serve` answers `refused` with
`details.reason: "mcp_disabled"`.

## Credentials

The tool needs two credentials from <https://trello.com/power-ups/admin>: an **API key** and a
**token** generated for that key. They are sent only in the `Authorization` header, never in URLs or
logs. Every output stream is redacted before it is written.

**Never put a credential on the command line.** `--api-key`, `--token` and similar flags are refused
before anything else is parsed, and the refusal tells you to rotate the credential.

For each credential (`API_KEY`, `API_TOKEN`) the first match wins:

1. `TRELLO_API_KEY_<PROFILE>`: the value, for the active profile
2. `TRELLO_API_KEY_FILE_<PROFILE>`: a path to a file holding it, for the active profile
3. `TRELLO_API_KEY`: the value
4. `TRELLO_API_KEY_FILE`: a path to a file holding it
5. the config file's active profile: `api_key_file` (a path) or `env_file`
6. the config file's `default` section, in the same forms

Credential files (including `env_file`) must be mode **0600 or 0400**. Anything looser is a `config`
error, not a warning. The config file holds **paths only**: a config file containing a credential
value is refused, so it is always safe to read or commit.

The recommended setup on a workstation:

```sh
mkdir -p ~/.secrets && chmod 700 ~/.secrets
printf 'TRELLO_API_KEY=%s\nTRELLO_API_TOKEN=%s\n' '<key>' '<token>' > ~/.secrets/trello.env
chmod 600 ~/.secrets/trello.env
```

and in the config file, `"default": {"env_file": "~/.secrets/trello.env"}`. Then confirm it works:
`trello doctor`.

## Settings and the config file

`trello teach config` is the authoritative reference: every setting, its environment variable, flag
and config-file member, the precedence rules, platform paths, and a worked example.
`trello list-config` shows the value in effect for each setting and where it came from.
`trello list-config --schema` prints the config file's JSON Schema.

Summary of the settings:

| Setting | Default | Notes |
|---|---|---|
| `CONFIG` | platform default | `TRELLO_CONFIG` or `--config`; a named file must exist |
| `PROFILE` | none | `TRELLO_PROFILE` or `--profile` |
| `LIMIT` | 25 | records per call; each command has a maximum |
| `MAX_BYTES` | 32768 | response size cap |
| `MAX_STRING`, `MAX_DEPTH` | 2048, 8 | per-string and nesting caps |
| `MAX_PAGES` | 10 | upstream requests per call |
| `TIMEOUT` | 30s | per upstream request |
| `BUDGET` | 60s (600s with `--all`) | per call, including retries |
| `DATASET_DIR` | platform cache dir | where `--all` datasets are stored |
| `DATASET_TTL` | 1h | sliding: each read extends it |
| `DATASET_MAX_BYTES`, `DATASET_MAX_RECORDS` | 256MB, 1000000 | per dataset |
| `DATASET_TOTAL_BYTES` | 2GB | the directory; least recently read are evicted first |
| `LOG_LEVEL` | error | `off` `error` `warn` `info` `debug`; `--verbose` = debug; stderr, JSON lines |
| `BASE_URL` | `https://api.trello.com/1` | overrides are honoured **only** for loopback hosts (test doubles) |

Resolution order for every setting: flag, `TRELLO_<SETTING>_<PROFILE>`, `TRELLO_<SETTING>`, the
config file's profile, the config file's `default`, `AGENTCLI_<SETTING>` (shared by tools in this
family), then the built-in default. Durations and sizes need units (`30s`, `4h`, `256MB`).

Default locations: the config file is `<config base>/agentcli/trello/config.json` and datasets are in
`<cache base>/agentcli/trello/datasets/`:

| Platform | Config base | Cache base |
|---|---|---|
| Linux and other Unix | `$XDG_CONFIG_HOME`, else `~/.config` | `$XDG_CACHE_HOME`, else `~/.cache` |
| macOS | `$XDG_CONFIG_HOME` if set, else `~/Library/Application Support` | `$XDG_CACHE_HOME` if set, else `~/Library/Caches` |
| Windows | `%APPDATA%` | `%LOCALAPPDATA%` |

With no home directory (containers, service accounts), set `TRELLO_CONFIG` and `TRELLO_DATASET_DIR`;
the tool refuses to guess.

## Profiles

A profile is a named, **partial** set of overrides, usually one per Trello account:

```json
{
  "config_version": 1,
  "default":  { "env_file": "~/.secrets/trello.env" },
  "profiles": {
    "work": { "dataset_ttl": "4h", "api_token_file": "~/.secrets/trello-work.token" }
  }
}
```

`--profile work` (or `TRELLO_PROFILE=work`) changes only what `work` defines; every other setting keeps
its value from the lower tiers. A profile can also exist only in the environment
(`TRELLO_API_TOKEN_WORK=…`). In variable names a profile is uppercased, with `-` becoming `_`.
Selecting an unknown profile is a `config` error, and a profile may not be named `default` or
`FILE`.

`trello list-profiles` shows what you can choose. It returns `[]` when no profile is declared, so
there is nothing to pass to `--profile`. Otherwise it lists `default` (running with no `--profile`)
first, then each profile, with `active` marking the one in effect and `declared_in` showing whether
it comes from the config file, the environment, or both. It lists names only, never values.

## Datasets

`--all` on a list command fetches the whole result set, stores it as JSON Lines under `DATASET_DIR`,
and returns the first part. The agent then pages through it with `dataset read`, which never
contacts Trello. Datasets are owner-only (directory 0700, files 0600), redacted like every other
output, and expire `DATASET_TTL` after their last read. Expiry and eviction run after a command has
answered. A failure there never fails the command.

## Writes and the attach-file risk

The full build adds `cards create`, `move`, `update`, `comment`, `attach-url`, `attach-file`,
`archive` and `delete`. Every one:

- is refused without `--confirm`, and the refusal shows the exact request as a preview (credentials
  redacted), with nothing sent;
- sends nothing with `--dry-run`, even alongside `--confirm`;
- is never retried automatically. A timeout reports `details.write_state: "unknown"`, because Trello
  has no idempotency keys and a retry could act twice.

`cards update` **replaces** the description; it does not append.

`cards delete` is **permanent**: Trello removes the card with its comments and attachments, and
there is no undo. `cards archive` hides the card and can be reversed in Trello; `teach` tells agents
to prefer it. If agents should never delete, run `trello-ro`, or deny `cards delete` by name in the
agent's permission rules.

**`cards attach-file` uploads any local file the process can read** to Trello, where anyone with
access to the board can download it. The confirmation preview shows the resolved path, the name
sent and the size, never the content. That helps a careful agent, but it does not protect you from
a careless or manipulated one. If the host has files that must not leave it (SSH keys, cloud
credentials, source trees, other agents' secrets), do one of these:

- run `trello-ro`;
- run the full build as a user that can read only what it should, or in a container with nothing
  sensitive mounted;
- use a Trello token without write scope.

Trello's per-file size limit depends on the plan. Oversize uploads come back as `validation` from
Trello; the tool does not guess the limit locally.

## MCP server mode

`trello serve` runs the same binary as a Model Context Protocol server. Each command becomes a tool
with the same name, parameters and JSON envelope. `tools` and `describe` are left out because the
protocol lists tools itself. `teach` is offered. Per-call flags become arguments with underscores
(`--max-bytes` → `max_bytes`), and write tools take `confirm` and `dry_run`. A read-only build lists
no write tools.

`serve` accepts only server flags: `--transport`, `--addr`, `--allow-remote`, `--token-file`,
`--tls-cert-file`, `--tls-key-file`, `--tls-terminated-upstream`, `--profile`, `--config`,
`--verbose`, `--deterministic`. The profile, config and determinism you start it with apply to every
call. Trello credentials are resolved per call, so `initialize` and the tool list work before any
credential is configured.

### stdio (local agents)

```sh
trello-ro serve
```

The client starts the process and talks over stdin/stdout. Nothing but protocol messages is written to
stdout; the startup and shutdown lines go to stderr. Example for Claude Code:

```sh
claude mcp add trello -e TRELLO_CONFIG=$HOME/.config/agentcli/trello/config.json -- /usr/local/bin/trello-ro serve
```

### Streamable HTTP

```sh
trello-ro serve --transport http                       # http://127.0.0.1:7810
trello-ro serve --transport http --addr 9000           # a bare port still binds loopback
```

The MCP endpoint is any path except `/health`; clients normally use `http://127.0.0.1:7810/mcp`.

#### What every HTTP listener enforces

| Request | Response |
|---|---|
| `GET /health` | `200`, unauthenticated: `tool`, `tool_version`, `contract_version`, `writes_enabled`, `mcp_enabled`. Nothing else: no addresses, paths, credentials or command list |
| `Origin` header present and not loopback | `403` (blocks web pages reaching a local server through DNS rebinding). No `Origin` is allowed |
| Body over 1 MiB | `413` |
| Token configured and missing or wrong | `401`, no detail. Compared in constant time |
| New session when 64 are open | `503`. Sessions idle for 30 minutes are closed |

**Not offered over HTTP**, because they act on the server's own filesystem or configuration, which a
remote client does not share: `dataset.clear`, `dataset.rm`, `doctor`, `list-config`. `dataset.read`,
`dataset.list` and `dataset.stat` are offered, and `meta.dataset.path` is left out of results.

#### The bearer token

Optional on loopback, **required** on any other address. Sources, first match wins:

1. `TRELLO_MCP_TOKEN`: the value
2. `TRELLO_MCP_TOKEN_FILE`: a path
3. `--token-file <path>`

Never pass the token itself as an argument. Token files must be mode 0600 or 0400.

```sh
openssl rand -hex 32 > ~/.secrets/trello-mcp.token && chmod 600 ~/.secrets/trello-mcp.token
trello-ro serve --transport http --token-file ~/.secrets/trello-mcp.token
```

Clients send `Authorization: Bearer <token>`:

```sh
claude mcp add --transport http trello http://127.0.0.1:7810/mcp --header "Authorization: Bearer $(cat ~/.secrets/trello-mcp.token)"
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
from the environment: `TRELLO_MCP_TLS_CERT_FILE`, `TRELLO_MCP_TLS_KEY_FILE`, and
`TRELLO_MCP_TLS_TERMINATED_UPSTREAM=true`.

Why plaintext is refused by default: a bearer token over plain HTTP can be read by anything on the
network path and replayed. The upstream Trello credential sits behind that token.

When writes are enabled on a non-loopback address, the server prints a warning recommending the
read-only build. Take the advice.

#### Option A: the tool serves HTTPS, with a certificate from mkcert

[mkcert](https://github.com/FiloSottile/mkcert) creates a private certificate authority and
certificates it signs. It suits a home lab or a small team, without a public domain.

On the **server**:

```sh
mkcert -install                                   # creates the local CA (once)
mkdir -p /etc/trello/tls && cd /etc/trello/tls
mkcert -cert-file cert.pem -key-file key.pem trello.lan 192.168.1.20 localhost 127.0.0.1
chmod 600 key.pem                                 # required: a looser key file is refused
mkcert -CAROOT                                    # prints where rootCA.pem lives
```

List every name and IP clients will use, because the certificate is checked against them. At startup
the tool loads the pair, checks that the key matches the certificate and that the certificate is
currently valid, and refuses to start otherwise. A bad file fails at startup, not on the first
connection.

```sh
TRELLO_MCP_TOKEN_FILE=/etc/trello/mcp.token \
  trello-ro serve --transport http --addr 0.0.0.0:7810 --allow-remote \
    --tls-cert-file /etc/trello/tls/cert.pem --tls-key-file /etc/trello/tls/key.pem
```

On each **client** machine, trust the CA: copy `rootCA.pem` (never `rootCA-key.pem`) from the server's
`mkcert -CAROOT` directory and either run `CAROOT=<dir> mkcert -install` there, or add it to the
client runtime's trust store. For Node-based clients: `NODE_EXTRA_CA_CERTS=/path/rootCA.pem`. Then:

```sh
curl --cacert rootCA.pem https://trello.lan:7810/health
```

Certificates from a public CA (Let's Encrypt) or your organisation's CA work the same way and need
no client-side trust step.

#### Option B: TLS terminated in front of the tool

Behind nginx, Caddy, Traefik, a Kubernetes ingress, a service mesh with mTLS, or an encrypted overlay
such as Tailscale or WireGuard:

```sh
TRELLO_MCP_TOKEN_FILE=/etc/trello/mcp.token \
  trello-ro serve --transport http --addr 0.0.0.0:7810 --allow-remote --tls-terminated-upstream
```

The tool serves **plaintext**, and its startup line says so:
`"tls":"plaintext; TLS terminated upstream (operator-declared)"`. The tool cannot check your claim.
Make sure the listener is reachable only from the proxy (bind to the private interface, a
Kubernetes `ClusterIP`, or a firewall rule). The bearer token is still required, and the proxy should
pass the `Authorization` header through unchanged.

#### Startup and shutdown lines

On stderr, one JSON line each, containing no secrets:

```json
{"level":"info","msg":"serve started","transport":"http","addr":"127.0.0.1:7810","writes_enabled":false,"auth":"on","tls":"plaintext on loopback"}
```

`tls` is one of `https`, `plaintext on loopback`, or
`plaintext; TLS terminated upstream (operator-declared)`. SIGINT or SIGTERM shuts the server down
gracefully; the exit status is 130 or 143.

## Running in a container

The `Dockerfile` builds the **read-only** flavour by default (`--build-arg TAGS=mcp` for the full one)
onto `gcr.io/distroless/static-debian12:nonroot`: the binary only, running as a non-root user with no
shell. That user has no home directory, so the image sets `TRELLO_CONFIG=/config/config.json` and
`TRELLO_DATASET_DIR=/data/datasets`. Mount `/data` writable and the config read-only:

```sh
docker run --rm \
  -v "$PWD/config.json:/config/config.json:ro" \
  -v "$HOME/.secrets/trello.env:/secrets/trello.env:ro" \
  -v trello-data:/data \
  trello-ro boards list
```

with `"env_file": "/secrets/trello.env"` in the config. A mounted credential file must still be 0600
or 0400, and readable by the container's user (uid 65532).

To serve MCP from a container, remember that `0.0.0.0` inside the container counts as remote, so all
three requirements apply:

```sh
docker run --rm -p 127.0.0.1:7810:7810 \
  -v "$PWD/config.json:/config/config.json:ro" -v "$HOME/.secrets:/secrets:ro" -v trello-data:/data \
  -e TRELLO_MCP_TOKEN_FILE=/secrets/trello-mcp.token \
  trello-ro serve --transport http --addr 0.0.0.0:7810 --allow-remote --tls-terminated-upstream
```

This is acceptable when the published port binds to the host's loopback, as above, or sits behind a
TLS proxy. Otherwise mount a certificate and use `--tls-cert-file`/`--tls-key-file`.

## Checking an installation

```sh
trello version          # flavour: writes_enabled, mcp_enabled
trello doctor           # config, credentials, network, authentication, clock skew, dataset directory
trello list-config      # every setting and its source
trello teach            # what an agent sees first
```

`doctor` exits non-zero when something an agent would hit is wrong, and says what to do about it.
