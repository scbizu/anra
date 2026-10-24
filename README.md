# Anra

> Anra do not intend to accept any PRs , it is my own agent.

Anra is another multi-agent (or agent team) implementation upon the [tape-go](https://github.com/scbizu/tape-go), which is inspired by [tape](https://tape.systems).

Agents can communicate with peers and share relevant context with permission.

- [Command line usage](docs/cli.md)
- [Agent team design](docs/agent-team.md)
- [Peer integration](docs/a2a-integration.md)
- [API and development workflow](proto/README.md)
- [BDD end-to-end scenarios and testing policy](docs/testing.md)

## Say hello between two running agents

Build the CLI with `mise run build-cli`. Set `DEEPSEEK_API_KEY` for both agent
servers, then start the shared directory:

```sh
./anra registry --listen 127.0.0.1:9090
```

Start Alice and Bob in separate terminals using [anra.example.toml](anra.example.toml):

```sh
./anra --config anra.example.toml serve --agent alice
# Another terminal:
./anra --config anra.example.toml serve --agent bob
```

Each server chooses an available port and registers its Agent Card. With both
running, send in either direction from another terminal:

```sh
./anra --config anra.example.toml peers
./anra --config anra.example.toml send --agent alice --to bob "hello from alice"
./anra --config anra.example.toml send --agent bob --to alice "hello from bob"
```

The CLI contacts the source agent's running server. That server discovers the
destination by name and sends directly over A2A. Both agents persist the exchange
in separate tapes under `.anra/`. No peer addresses are needed in the config.
Ctrl-C unregisters a server; crashed servers disappear when their leases expire.

The registry and agent endpoints run inside a trusted network. No shared peer
token is required. Requests carry the caller's agent identity; team members are
trusted not to impersonate one another. TapeView borrowing still enforces grants,
selected ranges, expiry, and release for each caller.

Use `ask` to let Alice's model discover Bob and send the greeting through its
built-in tools:

```sh
./anra --config anra.example.toml ask --agent alice "Discover Bob and ask him to say hello."
```

Agents run tape-go's ADK runtime with the DeepSeek provider. Tools are built in;
the CLI and config do not accept injected tools. Viper accepts `ANRA_` environment
settings; precedence is flags, environment, agent TOML settings, shared defaults,
then built-in defaults. The E2Es use a deterministic local model HTTP fixture
and need no real LLM key. See [command line usage](docs/cli.md).
