# A2A Tape View Borrowing Extension

Status: v1 implemented in `pkg/a2a`; RPC names and schemas below are a project
extension, not A2A core methods. See [integration and limits](../a2a-integration.md).

## Project ownership

This proposal belongs to anra, the personal agent composed of persistent team
members with domain memory and ownership. Each member has its own tape and
collaborates with peers without a mandatory main-agent role. Member identity,
directory capabilities, peer selection, permission policies, borrowing RPCs,
and borrowed-context consumption are anra responsibilities.

Tape-go remains the general foundation for append-only records, ranges,
retrieval, context management, and execution integration. This proposal requires
no team-specific types, RPC methods, directory rules, or permission policies in
tape-go. References to tape-go APIs below describe dependencies consumed by
anra; the extension dispatcher and wire DTOs are implemented in anra.

Context: [Agent 团队与个人智能体](session://5df05489-4aaf-4114-ad52-664e544281cb).
Migrated from tape-go's design draft. The full specification and Mermaid diagram
are retained; the provisional extension URI now belongs to anra and the historical
storage-profile reference points to the archive in this repository.

## Problem and boundaries

Every agent lives on its own tape. An agent needing context discovers a peer's
Agent Card and requests permission to borrow relevant entries. The lending peer
selects the best permitted context view of its tape. The initial selection is
one anchor-backed range; the view envelope retains a ranges array for future
disjoint views. There is no main-agent role or shared tape.

The former tape-go A2A task store persisted protocol objects but did not implement
context borrowing. Local `view.TapeView` represents one range; `EntryView` JSON
serialization emits raw entries without range metadata. Neither is the wire
contract for a permissioned, disjoint remote view.

A2A supports Method Extensions declared in Agent Cards and activated per
request. This design uses that mechanism and preserves core A2A operations.
Registry discovery is independent: a registry or configured URL locates a card;
the lending peer makes every context-access decision.

## Interaction

```mermaid
sequenceDiagram
    participant A as Agent on Tape A
    participant D as Optional directory
    participant B as Agent on Tape B
    participant P as B's permission and selection policy
    participant S as B's tape storage
    A->>D: Discover relevant peer card URLs
    D-->>A: Best matching peer card URL, or no match
    A->>B: Fetch Agent Card
    A->>B: TapeViewService/Borrow (activate extension)
    B->>P: Authenticate requester; select best permitted context range
    P-->>B: Grant or denial
    B->>S: Capture source identity and head
    B-->>A: Granted view, denied, or pending decision
    A->>B: TapeViewService/Read (viewId, optional cursor)
    B->>P: Check grant, requester, expiry and revocation
    B->>S: Read only granted ranges within captured head
    B-->>A: Entry page with source provenance
    A->>A: Consume context; retain source provenance
```

## Extension declaration and activation

Use the versioned project identifier `urn:anra:a2a:tape-view:v1`. Declare it in `capabilities.extensions`, with
`required: false` when ordinary A2A operations work without it. Extension params
declare its RPC endpoint and supported entry media types.

Clients request activation using `A2A-Extensions`. The server acknowledges
activation in the response header. Activation enables protocol behavior; it
does not grant access to tape entries. Unsupported or inactive extension calls
fail explicitly.

## Directory peers and decision models

A directory can itself be a tape-owning peer. Its tape records registrations,
card updates, withdrawals, and discovery decisions; a rebuildable projection
contains the currently effective registrations. This capability introduces no
agent hierarchy. Multiple directories can serve different communities. A
configured peer or directory card URL provides the initial discovery seed.

Decision models have two distinct relevance-selection roles:

- A directory ranks visible, current peer capability descriptions against the
  requesting agent's problem and returns the best matching Agent Card reference,
  or no match.
- A lending peer ranks its permitted context candidates against the borrowing
  query and selects the single best anchor's source range for the grant.

Eligibility checks precede ranking: directory visibility and registration
validity for discovery; requester permissions and exportability for lending.
Relevance scores never confer authority. Only information permitted for use by
the configured decision provider is submitted to that provider. A lender may
add a model-assisted purpose assessment within its explicit sharing policy;
the current classifier is a relevance interface, not an authorization API.

The existing `decision.Classifier` returns candidate indexes, scores, and
confidence; `pkg/provider/jev.Client` implements it. A directory needs an
explicit representation of card capabilities as classifier candidates and a
stable mapping back to peer identities. The lender can map decision anchor
candidates back to their source scopes. `AnchorDecider.ShouldAnchor` remains a
checkpoint decision, not a registration or access decision.

The current `decision.Finder.FindAll` selects only the single best anchor,
despite its plural name. This matches the initial borrowing selection policy;
no multi-candidate finder change is required.

`Ranker` owns relevance ordering and the cut-off. It returns candidate indexes,
best first, omitting candidates that are not relevant enough; scores are an
internal detail of score-based implementations. `ScoreRanker` adapts a scoring
decision API: highest score, then highest confidence, with stable candidate
order breaking exact ties, then an optional `MinScore` threshold and `Limit`
(top-N). Discovery (`RankPeers`) returns the whole short list, since a borrower
need not visit every discovered peer. Borrowing grants only the first-ranked
context range; granting multiple ranges is deferred. An empty result means no
match. `Ranker` and `Scorer` are trusted to return in-range, unique indexes and
finite scores; the client that owns a vendor protocol (for example the JEV
client, which derives indexes from its own request) parses the vendor response
into that shape. Indexes only select among candidates already permitted, so a
faulty ranker cannot reach unpermitted context. Scores are
comparable within a classifier call; they are not assumed comparable across
directories or separate calls, so a threshold needs a calibrated scorer.

Provider failures remain errors or pending decisions according to the owning
peer's policy; they do not widen permissions or substitute arbitrary context.
Decision records can retain candidate references, selected results, and the
provider/model identity for provenance without duplicating private payloads.

## RPC contract

All methods use generated protobuf/Connect bindings over HTTP and require an
authenticated requester. The canonical contract is
`proto/anra/a2a/tape/v1/tape.proto`; the handwritten extension JSON-RPC transport
has been removed.

| Method | Input | Result |
| --- | --- | --- |
| `TapeViewService/Borrow` | `requestId`, `purpose`, relevance `query`, optional requested bounds | `granted` with view, `denied`, or `pending` with request ID |
| `TapeViewService/Get` | `requestId` | Current decision, including an existing grant when approved |
| `TapeViewService/Read` | `viewId`, optional opaque cursor, bounded page size | Ordered entry page and optional next cursor |
| `TapeViewService/Release` | `viewId` | Idempotent acknowledgement that this requester has released the grant |

`borrow` asks the peer to select context, rather than assuming the requester
knows its sequence numbers. Requested bounds can narrow selection but never
authorize access. The peer can decide through its own agent or application
policy. `pending` supports decisions that outlive one HTTP request; no A2A Task
is synthesized merely to represent a view.

Repeated `requestId` values for the same requester and identical input recover
the same decision. Reusing one with different input is a conflict. A denied
request discloses no ranges or entries.

## Granted view and entries

Example view; all sequences are canonical decimal strings:

```json
{
  "viewId": "opaque-grant-id",
  "source": {"agentId": "peer-stable-id", "tapeId": "opaque-tape-id"},
  "snapshotHead": "24",
  "ranges": [{"start": "1", "end": "5"}],
  "expiresAt": "2026-10-07T00:00:00Z"
}
```

The initial contract returns at most one range. Ranges are half-open
`[start,end)`, with `start >= 1`, `start < end`, and
`end <= snapshotHead + 1`. Multiple disjoint ranges require a future negotiated
contract change; retaining an array does not imply initial support for them.
An empty approved selection returns an empty ranges array. The source identity
is stable for the grant and opaque to the requester; it does not expose or
accept local owner routing. Sequence IDs are meaningful only with this source.

An entry page includes `viewId`, source identity, snapshot head, entries, and
`nextCursor` when more entries exist. Each entry carries `seq`, `kind`,
`timestamp`, a readable `summary`, and an explicitly typed payload
(`mediaType` and `data`) if granted. Arbitrary local `CustomEntry.Extensions`
are never automatically exported. Typed export codecs define what can be
shared, including messages and artifacts; unsupported types fail explicitly.

Entries remain in source sequence order across ranges. Borrowed entries are
context supplied by a peer, not authenticated local assistant actions or tool
executions. If recorded locally, they receive local sequence IDs and retain the
source agent, tape, sequence, and view ID as provenance.

## Permission, consistency and failure behavior

Grants are bound to the authenticated requester, source, selected ranges,
snapshot head, allowed payload representation, and expiry. A view ID is a
reference, not a bearer credential. Every read rechecks the grant; expired,
revoked, or released grants fail. A cursor cannot expand access and is bound to
the grant and its snapshot.

The append-only snapshot excludes entries appended after approval. If storage
has reset, changed source identity, or cannot return the granted entries, reads
fail rather than substitute a different tape. Grant decisions and request
idempotency must survive restart; they are separate from the existing A2A task
projection. Already delivered context cannot be recalled by revocation.

Responses distinguish pending decisions, denials, invalid params, request-ID
conflicts, unavailable views, and storage failures. Concrete extension error
codes and payload size/page limits are fixed in the [integration contract](../a2a-integration.md). Errors
for unauthorized view IDs must not reveal another requester's grant details.

## Integration and compatibility

The former tape-go adapter used `a2a-go/v2 v2.3.1`. That SDK's JSON-RPC handler switches over core methods and
returns method-not-found for unknown ones. Call interceptors do not add method
dispatch. A project dispatcher must route extension methods and delegate core
requests intact to the official handler, preserving streaming and authentication.
The implementation uses an explicitly advertised separate extension endpoint,
compatible with the extension model, and delegates core requests to the SDK
without inspecting or rewriting their bodies.

The wire view is a new DTO, not a change to local `TapeView`, `EntryView`, or
profile-v1 task records. No current storage record migration is required by
this proposal. Standard clients can continue using core A2A; tape-aware clients
negotiate this extension before borrowing.

## References

- [A2A extensions: method extensions, declaration and activation](https://a2a-protocol.org/latest/topics/extensions/)
- [A2A discovery: well-known cards, registries and direct configuration](https://a2a-protocol.org/latest/topics/agent-discovery/)
- [Historical A2A tape persistence profile](a2a-tape-storage-profile-design.md)
- [Historical A2A list pager design](a2a-list-pager-review-design.md)

## Implemented v1 decisions

The extension identifier is `urn:anra:a2a:tape-view:v1`. The native Agent Card
advertises `/anra.a2a.tape.v1.TapeViewService` as its single Connect service
endpoint; `/a2a` remains the SDK's core endpoint. The integration pins the official Go SDK at v2.5.0.

Grants and decisions are persisted in a dedicated indexed bbolt journal, separate from
source context. Source identity is provisioned by the application; a grant
references the tape by that identity, snapshot head and range. The tape is
append-only, so the reference identifies the content and grants carry no
session, exporter or content fingerprints. The journal holds three record kinds:
a grant (immutable), request state (pending or denied) and a release. The
default exporter shares only
user/assistant summaries; other payloads require a deterministic typed exporter.
Limits and error codes are fixed in the integration contract linked above.

Configured card discovery, best-peer ranking, permissioned best-range ranking,
borrowing RPCs, and Anra tool-result consumption are implemented. Network
directory registration and a concrete JEV adapter remain future integrations;
`Ranker` and `Eligible` provide their boundaries. The historical A2A task-store
adapter has not been restored.

## Trusted-team identity profile

The current trusted-team deployment restricts the registry and agent endpoints
at the network boundary. It uses the caller-supplied `Anra-Agent` identity in
place of a global bearer token, trusting members not to impersonate each other.
References above to an authenticated requester use that established identity
in this profile. Borrow, read, and release still check the requester-bound grant;
a view ID alone does not confer access. Credential verification can be supplied
by a different handler callback in deployments that require it.
