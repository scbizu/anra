# Anra agent team

Anra composes agents on tape-go, pinned to `v0.2.0-anra.2`. Each member owns an identity, role instructions,
an ADK runtime, built-in tools, and a Tape. Members communicate through A2A and can borrow
another member's Tape view when authorized.

## Member runtime

`agent.New(principal, storage, options...)` configures a member. Storage must
be an initialized tape-go `*bbolt.Bbolt`. The constructor accepts that concrete
type directly. The caller owns initialization and closure.
`Run(ctx, input)` delegates the model/tool loop to tape-go's `agent.NewRuntime`
and ADK runner. `agent.WithLLM` supplies the provider model, implementing ADK's
`model.LLM`; provider selection and agent execution are separate concerns.
Tape-go's `TapeAdapter` implements ADK's session and memory services and context
window callback. User messages, model responses, tool calls, and tool results
are persisted as ADK events and reconstructed on later invocations.

Tools are registered internally. The runtime includes tape-go's native Bash
tool and Anra's peer discovery/messaging tools. `WithPeers` supplies their
network services; `WithBorrowClient` connects the built-in context borrowing
capability. There is no public option for injecting arbitrary tools. Anra does
not maintain a second agent loop. Its storage adapter preserves tool entry kinds
and reads legacy structured messages as ADK events without rewriting old tapes.
The former `Submit` and `Inspect` lifecycle APIs have been removed; callers use
`Run` for execution and Tape storage for history queries.

Conversation reconstruction does not resume interrupted tool execution or
provide exactly-once side effects. A failed Run returns its error to the caller.

## Collaboration

`a2a.NewExecutor` exposes a member's Run method through A2A. Peer messaging and
permissioned Tape borrowing belong to `pkg/a2a`; members need no shared team
Tape or mandatory coordinator. A coordinating member can use tools to request
work from peers.

The CLI can run every member as a server. A shared `registry` service tracks
leased Agent Cards; each server registers and renews its own record. The CLI
invokes the selected source server for named sends, and that server discovers
and contacts the destination directly. The directory routes no messages and
owns no agent tapes. `Anra.Exchange` records explicit outgoing messages and
remote answers while allowing incoming requests during the network call.

## Trust model

An Anra team is a set of trusted members. Access to both the registry and agent
endpoints is restricted by the deployment network. There is no global shared
peer token. Peer requests carry `Anra-Agent`, a stable caller name supplied by
the client, which members are trusted not to impersonate. The CLI uses its selected
agent name; receiving endpoints validate the name's format, without a live
registry membership lookup. The directory's per-registration lease still protects
renewal and removal of active names.

Tape is not opened to external parties, so A2A responses from a member are
treated as well-formed and honest.
The tape-view client checks only what it needs to proceed and does not defend
against a hostile peer:

- Responses are checked against the protobuf schema (Protovalidate) and nothing
  more. Decision correlation, timestamp and range validity, payload JSON syntax,
  and the `view_id`, `source` and `snapshot_head` echoed on each page are not
  re-validated.
- `ReadView` still checks entry order and range coverage, because it needs them
  to assemble the granted range and to stop paging.
- The service still requires a requester identity and enforces requester-bound
  grants, expiry and export limits. Trust in peers does not widen what a member
  may read.

External or untrusted tapes would need these client checks restored, together
with per-peer credentials and redirect handling. That is out of scope today.

Tape storage, View handling, and memory checkpointing/retrieval belong to
tape-go. Its decision extension can be configured at the storage boundary;
Anra does not duplicate its memory policy or index.

See [A2A integration](a2a-integration.md) for the protocol and permission model.
