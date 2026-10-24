# Anra CLI

Build with `mise run build-cli`, or `go build -o anra ./cmd/anra`.
Run `./anra --help` or `./anra <command> -h` for Cobra-generated usage.
Shared flags (`--agent`, `--data-dir`, `--session`, `--config`, `--registry`) can precede the subcommand,
and command flags can appear before or after the message. Quote the message
to preserve its spacing. Use `--` before a message that starts with a dash.

Command definitions live in the `cmd` package: `root.go` registers the command
tree, with one file per subcommand, including `run.go`, `serve.go`, `send.go`,
`registry.go`, `peers.go`, and `ask.go`.
`cmd/anra/main.go` is the executable entry point.

## Agent configuration

Use `--config path/to/agents.toml` to keep named agents in a TOML file.
The repository includes [`anra.example.toml`](../anra.example.toml):

```toml
default-agent = "alice"

[defaults]
data-dir = ".anra"
session = "default"
provider = "deepseek"
timeout = "1m"
registry = "http://127.0.0.1:9090"
listen = "127.0.0.1:0"

[agents.alice]

[agents.bob]
```

Select an entry with `--agent`; omitting it selects `default-agent`, or `anra`
if no default is configured. The selected agent must exist in the file.
Add, edit, or remove `[agents.<name>]` sections to manage the agent list.

Start the directory with `./anra registry`.
Set `DEEPSEEK_API_KEY` in both server environments, then start the agents in separate terminals:

```sh
./anra --config anra.example.toml serve --agent alice
./anra --config anra.example.toml serve --agent bob
```

From another terminal:

```sh
./anra --config anra.example.toml send --agent alice --to bob "hello from alice"
```

Each `[agents.<name>]` section and `[defaults]` can contain `data-dir`, `session`,
`provider`, `api-base`, `model`, `system`, `listen`, `registry`, and `timeout`. All values are
strings, with durations such as `"30s"` or `"2m"`. Only settings applicable to
the current command are applied; for example, `serve` uses `listen`.
`registry` is the shared discovery endpoint. Sends select a registered
destination by name with `--to`; agent profiles contain no peer addresses.

Explicit flags take precedence over environment variables, agent settings,
`[defaults]`, and built-in defaults, in that order. This includes explicitly
empty values and values equal to built-in defaults. Viper resolves these sources directly. Commands and agent construction read
the same settings instance; resolved values are not copied into option structs
or written back to Cobra flags. Duration values and required settings are
validated before execution. For example:

```sh
./anra --config anra.example.toml run --agent bob --session scratch "hello"
```

Relative `data-dir` values in the file resolve from the config file's directory;
an explicit `--data-dir` or `ANRA_DATA_DIR` resolves from the current working
directory. The model credential comes from `DEEPSEEK_API_KEY`, bound through Viper.
Malformed TOML, unknown keys, and unknown agent selections fail before storage
opens. No config file is loaded unless `--config` or `ANRA_CONFIG` is supplied,
and help does not require a readable config file.

## Environment variables

Viper binds command options to `ANRA_` environment variables, with hyphens
replaced by underscores. Variables apply to the commands that support the
corresponding flags.

| Variable | Setting |
| --- | --- |
| `ANRA_CONFIG` | TOML configuration file |
| `ANRA_AGENT` | Agent name |
| `ANRA_DATA_DIR` | Tape database directory |
| `ANRA_SESSION` | Conversation session |
| `ANRA_PROVIDER` | Model provider (`deepseek`) |
| `ANRA_API_BASE` | Optional provider API base URL |
| `ANRA_MODEL` | Model name |
| `ANRA_SYSTEM` | System instruction |
| `ANRA_LISTEN` | Server or registry listen address |
| `ANRA_REGISTRY` | Team Agent Card directory URL |
| `ANRA_TO` | Named send destination |
| `ANRA_LEASE_TTL` | Registry lease lifetime (default `15s`, minimum `1s`) |
| `ANRA_TIMEOUT` | Command timeout, such as `30s` |
| `DEEPSEEK_API_KEY` | DeepSeek API key |

For example, run an agent without repeating command flags:

```sh
ANRA_AGENT=alice ANRA_PROVIDER=deepseek ./anra run "hello"
ANRA_CONFIG=anra.example.toml ANRA_AGENT=bob ./anra run "hello"
```

Flags override environment variables even when the flag equals its built-in
default. Empty environment values are also honored: an empty required setting
fails validation instead of falling back to TOML. Credentials remain
environment-only and are not accepted as flags or TOML settings.

## Local agent

```sh
./anra run --agent alice --provider deepseek "hello"
```

`run` opens a tape-backed Anra agent, appends the message, runs it, prints its
answer to stdout, and closes storage. A later command with the same agent,
data directory, and session reconstructs its conversation from disk.

The runner is tape-go's ADK runtime. `--provider deepseek` selects the model
provider; it does not replace the agent loop. Set `DEEPSEEK_API_KEY` in the
environment before using `run` or `serve`:

```sh
./anra run --agent alice --model deepseek-chat --system "Answer briefly." "hello"
```

Leaving `--model` empty uses tape-go's provider default. `--api-base` can override
the provider's HTTP endpoint. An absent key is an error. Production has no echo
provider; the repository E2Es run a deterministic provider HTTP fixture with a
fixture key, so no real model credentials are needed.

The runtime registers its built-in Bash, peer discovery, and peer messaging
tools internally. Named peer operations require a configured registry.
There is no `--tools` flag, TOML `tools` key, environment tool setting, or public
option for injecting arbitrary tools.

## Running servers and discovery

Start one team directory with `./anra registry --listen 127.0.0.1:9090`.
The directory and peer endpoints must be accessible only to trusted team members.
There is no global peer token or credential exchange. Clients automatically send
`Anra-Agent: <caller-agent-name>`; receivers require a valid identity and trust
members not to impersonate one another. The header is not a credential, and a
receiver does not verify membership against the registry on every request.
The local CLI acts on behalf of the agent selected with `--agent`.
TapeView grants remain bound to the supplied requester identity.
Each `serve --registry http://127.0.0.1:9090` registers its Agent Card, including
its actual bound address and capabilities. All agents share this discovery
endpoint; each agent owns its tape and communicates directly with peers.

`serve` publishes `/.well-known/agent-card.json` and A2A at `/a2a`.
`--listen 127.0.0.1:0` chooses an available port. `peers --registry <url>` lists
registered names and card URLs. Agent names are unique while their leases are
active; attempting to start another server with the same name fails.

Registrations renew every third of the lease lifetime. The directory defaults
to a `15s` lifetime, configurable with `registry --lease-ttl`. Ctrl-C or SIGTERM
shuts down HTTP, closes the tape, and unregisters the server. A crashed process
loses its registration after the lease expires. The directory is in memory;
active servers retry transient directory failures and register again after it
restarts. An invalid or conflicting registration stops the affected
agent server. An agent restart registers the same name at its new port and
resumes its saved conversation.

`send --agent alice --to bob` requires both named agents to be serving. The CLI
resolves Alice, invokes her outbound endpoint, and Alice discovers
Bob's current card before sending over the official A2A SDK. Alice records the
outgoing message and Bob's reply; Bob records the incoming message and his
answer. Both source and destination tapes stay owned by their server processes.
Bob can use the same path to send to Alice. Concurrent opposite-direction sends
are supported; concurrent exchanges can interleave in each agent's session.

`send` explicitly routes a message to the destination's model. The CLI needs
no model key; the destination server does.

`ask --agent alice "Discover Bob and ask him to say hello."` submits a request
to Alice's model over A2A. Her ADK loop can discover peer cards, call the built-in
`send_peer_message` tool, and use Bob's response in her answer. ADK persists the
model's tool calls and results to Alice's tape, including history used after a
restart. Both servers need model credentials. Context borrowing is also a
built-in library capability, connected with `agent.WithBorrowClient`.

Sending requires both source and destination servers to be registered. The CLI
uses `--registry` (or its TOML/environment setting) to find the source server;
the source resolves the destination's current Agent Card. There is no `--peer`
flag or TOML `peer` setting, and `ANRA_PEER` has no effect. A server started
without `--registry` publishes its card but cannot participate in named sends.
Use a concrete listen address reachable by other team members; that address
is advertised in its card.

## Storage and sessions

Every command accepts `--agent`, `--data-dir`, and `--session`. Defaults are
`anra`, `.anra`, and `default`. Agent names start with a letter or digit and
contain only letters, digits, dots, underscores, or hyphens.

Each agent uses `<data-dir>/<agent>.db`, with its identity as the local tape
owner. `--session` selects a conversation inside that database. Incoming peer
messages use the serving agent's configured session; they do not choose a local
owner or session through A2A IDs. All peers talking to that server contribute
to that conversation.

Only one process can open an agent database at a time. Sends use the
running source server, so they work while its database is open. Stop a server
before using `run` with that agent's database. Alice and
Bob run concurrently because their files are separate. `run`, `ask`, and `send` accept
`--timeout` (default `1m`).
