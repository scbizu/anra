# Peer A2A and tape borrowing

Anra owns peer communication and context sharing. Tape-go remains the local
append-only storage/runtime foundation. Each peer has its own runtime, owner,
and tape; there is no coordinator requirement or shared team tape.

`pkg/a2a` uses the official `a2a-go/v2 v2.5.0` SDK for core A2A JSON-RPC. The
optional `urn:anra:a2a:tape-view:v1` extension uses a generated Connect service
advertised as a single endpoint in the Agent Card. Its protobuf service and
generated Connect v2 bindings live under `proto/` and `gen/`. Borrowing does not
create A2A tasks.

## Lender setup

Initialize the source tape under the lender's **local** owner using tape-go's
bbolt backend, then configure a dedicated grant journal. `Config.Tape` is an
initialized `*bbolt.Bbolt`; `Config.Journal` is an `a2a.Journal` — the storage
contract — and `*journal.Store` from `pkg/model/journal` is the bundled bbolt
implementation, which owns its own database file. The journal is bound to the
source tape identity and is server configuration, never a value from a peer
request body. The caller initializes and closes the source tape, and either
opens the journal itself or lets the service do it.

Set either `Journal` or `JournalPath`, never both: `JournalPath` lets the
service open the file and `Service.Close` release it, while `Journal` shares one
store between services and leaves it the caller's to close.

The example imports `tapev1 "github.com/scbizu/anra/gen/anra/a2a/tape/v1"` and
`"github.com/scbizu/anra/pkg/model/journal"`.

```go
researcherTape, err := bbolt.NewBboltStorage("researcher-session", "researcher.db")
// Check err, then defer researcherTape.Close().
err = researcherTape.Init(owner.WithOwnerId(ctx, "researcher-local"))
source := &tapev1.Source{AgentId: "researcher", TapeId: persistentTapeIncarnationID}
grantJournal, err := journal.Open("grants.db", source)
// Check err, then defer grantJournal.Close().
// Keep the source session ID stable across restarts and check initialization errors.
views, err := a2a.NewService(a2a.Config{
    Source:  source,
    Owner:   "researcher-local",
    Tape:    researcherTape,
    Journal: grantJournal,
    Policy: a2a.BestRangePolicy{
        Eligible: permittedAnchorCandidates,
        Ranker:   decisionProvider,
        TTL:      time.Hour,
    },
})
// Check err after each constructor/configuration call.
executor, err := a2a.NewExecutor(researcherAnra)
core := a2asrv.NewHandler(executor)
err = a2a.Advertise(card, publicBaseURL + a2a.ViewPath)
handler, err := a2a.NewPeerHandler(card, core, views, a2a.TrustedPeerIdentity)
```

The caller supplies a native SDK Agent Card with its name, skills, input/output
modes, security schemes/requirements, and a JSONRPC interface at
`publicBaseURL + a2a.CorePath`. `NewPeerHandler` serves:

| Path | Behavior |
| --- | --- |
| `/.well-known/agent-card.json` | Public native SDK Agent Card |
| `/a2a` | Caller-identified core A2A JSON-RPC, including SDK streaming |
| `/anra.a2a.tape.v1.TapeViewService/` | Caller-identified protobuf/Connect extension RPCs |

The CLI and trusted-team profile use `a2a.TrustedPeerIdentity` to resolve
`Anra-Agent` into a stable requester identity, also propagated into SDK
`CallContext.User`. Use `a2a.TrustedHTTPClient(agentName)` on the client side.
Both registry and peer endpoints must be accessible only to trusted members;
this header is an assertion, not proof of membership, and receivers do not
perform a live membership lookup. No global shared token is required.

`Policy` independently decides who may borrow which context. Every grant and
subsequent read remains bound to the requester identity. For deployments that
need credential verification, the handler callback can instead verify credentials
and return a stable identity; the card must advertise that deployment's scheme.

`Eligible` returns only permitted, exportable candidate anchor summaries and
source ranges inside the request's `Bounds`. `Ranker` receives those summaries and the query and returns the indexes worth
considering, best first; it owns ordering and any cut-off (top-N or threshold).
The grant covers the first. An empty result denies; provider errors propagate
without selecting fallback private context. A decision API that returns scores
can be wrapped in `ScoreRanker{Scorer, Limit, MinScore}`: highest score, then
highest confidence, then original candidate order. This local adapter seam can wrap tape-go's decision classifier; A2A permission
policy remains in Anra.

## Borrower setup and model consumption

Discover configured, trusted peer card URLs. An optional directory can provide
these same references. `RankPeers` ranks visible native card skill descriptions
and returns the peers worth visiting, best first, or no match. The CLI's
`registry` command provides a leased team directory of Agent Cards.
`serve --registry <url>` registers automatically, and named sends resolve the
current destination card through that directory. See
[CLI discovery](cli.md#running-servers-and-discovery). The registry is an Anra
directory service; core A2A communication still uses the official SDK.

```go
peerHTTPClient := a2a.TrustedHTTPClient("borrower")
card, err := a2a.Discover(ctx, peerHTTPClient, peerCardURL)
client, err := a2a.NewClient(peerHTTPClient, card)
borrower, err := agent.New("borrower-local", borrowerTape,
    agent.WithLLM(providerModel),
    agent.WithBorrowClient(client),
)
output, err := borrower.Run(ctx, "Find the peer's storage concurrency findings")
```

The HTTP client supplies caller identity, timeouts and endpoint trust. The tool
accepts `requestId`, `purpose`, and `query`. It returns the decision and, for a
grant, all pages checked against its source, snapshot and sequence range. Anra persists the result as a local tool response,
with `agentId`, `tapeId`, `viewId`, source sequences and snapshot head intact.
Peer entries are context data inside that response; their roles never become
local assistant messages or locally executed tool calls.

`NewClient` uses the generated Connect binding, and `Advertise` declares its
service URL as the extension's `endpoint`. The handwritten tape-view JSON-RPC
transport and `/tape-views` route have been removed; update old peer cards to
`publicBaseURL + a2a.ViewPath`. No legacy fallback is provided. Core A2A continues
to use the official SDK. See [API schema and mise/Buf workflow](../proto/README.md)
for generation, error mappings, and schema evolution.

For pending decisions, retry the identical borrow request or use `Client.Get`.
The lender resolves pending requests with the local `Service.Resolve` method.
The borrower can release a grant with `Client.Release`; the lender can revoke it
with `Service.Revoke`. Neither resolve nor revoke is a remote borrower RPC.
The borrowing tool leaves the grant open until explicit release or expiry.

Core text communication uses the official `a2aclient.NewFromCard`. `NewExecutor`
bridges text-only user messages to `Anra.Run` and emits direct agent Messages;
non-text input and task continuation are rejected. Task-based executors and
custom task stores can be supplied directly to `NewPeerHandler` through SDK
interfaces. The historical tape-backed task persistence profile is not restored
by this release. Core message IDs are not a durable runtime deduplication key;
the extension's request IDs are.

For a messaging-only server, `NewMessageHandler(card, core, a2a.TrustedPeerIdentity)`
serves the same discovery and caller-identified A2A endpoints without a borrowing
service or grant journal. This is the handler used by [`anra serve`](cli.md).

## Wire contract and limits

Clients activate `A2A-Extensions: urn:anra:a2a:tape-view:v1` on every extension
request and require the acknowledgement header. Activation is not authorization.

The Connect server uses validate-go's server interceptor with Protovalidate
rules from `tape.proto`. Invalid required fields, byte lengths, page sizes, and
sequence bounds are rejected before the handler runs, with `invalid_argument`
and field violation details. View size remains the complete selected range;
there is no independent entry-count cap. Authorization and grant integrity
are enforced by `Service` separately from request validation.

`Service`, `Client`, policies, and exporters use generated protobuf messages
from `gen/anra/a2a/tape/v1` directly. Local requests use the same Protovalidate
rules as Connect requests; validation errors retain `ErrInvalidParams` and
expose `protovalidate.ValidationError`. Configuration uses
`go-playground/validator/v10` for dependencies; `Config.Source` uses protobuf
rules. Structural response rules also live in `tape.proto`, including source
identifiers, canonical sequences, required timestamps, range counts, and the
relationship between decision status and view presence. Go retains JSON syntax,
Timestamp validity, request correlation, and authorization/integrity checks.
Conversion is limited to tape-go ranges and the
journal's own record types. Journal records are JSON, independent of the proto
wire encoding, and keep unexported storage shapes private to `pkg/model/journal`.

| Method | Parameters | Result |
| --- | --- | --- |
| `Borrow` | `requestId`, `purpose`, `query`, optional `bounds` | granted view, denied, or pending |
| `Get` | `requestId` | Existing decision for this requester |
| `Read` | `viewId`, optional `cursor`, optional `pageSize` | Entry page with source provenance |
| `Release` | `viewId` | `{ "released": true }`, idempotent |

The canonical wire schema is `proto/anra/a2a/tape/v1/tape.proto`. Each method
has a generated request and response message; responses wrap `Decision` or
`Page`. All sequence fields are canonical unsigned decimal strings with
arbitrary precision. v1 supports at most one half-open range `[seq_s, seq_e)`,
plus an empty approved range list. Status uses `DecisionStatus`; times use
protobuf Timestamp. Exported entries contain only public projections and
allowed payload metadata, never local owner IDs or CustomEntry extensions.
`Payload.json_data` preserves raw JSON bytes and is base64 in ProtoJSON. The borrowing tool also uses ProtoJSON for decisions and pages. Journal JSON
retains its independent storage format.

Bounds can only narrow selection. A grant references the source identity, the
snapshot head and the granted range. The tape is append-only, so that reference
identifies the content: grants carry no session, exporter name or content
fingerprints. Appends do not extend a grant. Each read verifies the requester,
release, expiry and source.
A cursor is an opaque encoding of the grant ID, snapshot and offset; it can only
page within an independently authorized grant, never expand its range.

| Limit | Value |
| --- | --- |
| Default / maximum page size | 32 / 100 entries |
| View size | Complete range selected by the lender's turn/anchor policy; no independent entry-count cap |
| Maximum encoded entry | 8 KiB |
| Request / response body | 1 MiB |
| Maximum grant TTL | 24 hours |
| Request ID | 256 bytes |
| Purpose / query | 8 KiB each |

The default `TextExporter` exports user/assistant readable summaries as typed
`text/plain` JSON strings and never copies arbitrary custom extensions. Tool,
anchor, lifecycle and other custom entries require an explicit deterministic
`Exporter`; unsupported entry kinds fail the whole grant. If using another
exporter, update the advertised `entryMediaTypes` to its actual supported types.
The exporter is service configuration, not part of a grant; reads project the
granted range with the exporter configured at the time.

| Connect code | Meaning |
| --- | --- |
| `invalid_argument` | Malformed message or invalid parameters |
| `failed_precondition` | Extension inactive |
| `already_exists` | Request ID reused with different input |
| `not_found` | View unavailable (unknown, wrong requester, released or expired) |
| `internal` | Storage failure or masked internal/provider failure |
| `unimplemented` | Unknown RPC or unsupported entry representation |
| `unauthenticated` | Caller identity is missing or rejected |
| `resource_exhausted` | Oversized RPC message |

Errors never include another requester's grant details. `Get` and repeated
`Borrow` recover the recorded decision even after expiry/release; `Read` enforces
current availability. JSON-RPC envelopes, notifications and batches are no
longer part of the tape-view API.

## Persistence and operations

`Service` retains configuration and the journal store, with no in-memory grant
projection. The journal's records, indexes and transactions live in
`pkg/model/journal`, which uses [bolthold](https://github.com/timshannon/bolthold)
on bbolt; `Service` owns policy selection, export and RPC error mapping.
Construction performs no journal reads, and restart requires no replay.
`Borrow`/`Get` look up the requester/request-ID key; `Read`/`Release` resolve
view ID through the view index. Each lookup validates only the addressed record,
its key and source identity. Unrelated records and the audit trail are not
scanned on access.

The journal keeps a current state per request: pending, denied or granted. A
granted record is never rewritten; resolving a pending request replaces the
state, and a release is its own record keyed by view ID. Each update atomically
commits the state, its indexed view ID (or the release mark) and an append-only
audit record in one bbolt transaction, before responding. It checks the previous
state inside that transaction, rejecting stale decisions or releases with
`ErrConflict`. Pending/denied/granted decisions, request-ID idempotency and
revocations persist across restart. The service serializes its local policy
operations; services sharing a journal store use transactional state comparisons.
Separate processes cannot open the same journal file for writing while it is
locked.

Grant lookups use bolthold's on-disk index on the view ID, requiring O(log N)
database work and one decoded record per operation. The service does not retain
O(N) grant state. Each `Read` loads the selected range temporarily to export and
size-check it. Request retries and state transitions compare protobuf requests
and decisions with `proto.Equal`, without marshaling or hashing. tape-go's
`TapeStorage` interface has no keyed grant lookup or shared transaction API, so
the journal keeps its own database instead of using a finder.

A journal file is a bolthold store of a few record types, one bucket each:
`sourceBinding` (source tape identity and audit sequence), `requestState` (with
an index on `ViewID`), `grantRelease` and `auditEntry`. `Config.Journal` is an
`a2a.Journal`; the caller either opens the bundled `*journal.Store` with
`journal.Open`, which binds it to the configured source tape, or sets
`Config.JournalPath` and lets the service open and close that file. Another
implementation of the interface (for a server database) must apply decisions and
releases atomically and report conflicts and storage failures as
`journal.ErrConflict` and `journal.ErrStorage`. Files from the earlier layouts
(`anra:grant-journal/v2` buckets, or a tape-go tape journal with an `entries`
bucket) are rejected on access so saved decisions are never silently dropped;
start a fresh journal.

Provision `AgentID` and the opaque tape incarnation ID durably. Keep them across
normal restart; rotate the incarnation ID and start a fresh journal when replacing
or resetting a source tape. Grants rely on the append-only tape and perform no
content fingerprint checks, so a changed source tape with the same configured
identity is not detected; a missing entry in the granted range still fails the
read. Already delivered context cannot be recalled by closing a grant.

Run `mise run e2e` for the executable peer and CLI workflows, official SDK core
and streaming compatibility, model consumption/replay, bbolt restart,
idempotency, requester identity and grants, and invalid wire request checks.

References: [A2A extensions](https://a2a-protocol.org/latest/topics/extensions/),
[A2A discovery](https://a2a-protocol.org/latest/topics/agent-discovery/),
[design and ownership](designs/a2a-tape-view-extension-design.md).

Client validation boundaries and the simplification experiments are recorded in
[A2A client ablation](designs/a2a-client-ablation.md).
