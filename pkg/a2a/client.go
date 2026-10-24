package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	pb "github.com/scbizu/anra/gen/anra/a2a/tape/v1"
	"github.com/scbizu/anra/gen/anra/a2a/tape/v1/tapev1connect"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"
	runner "github.com/scbizu/tape-go/pkg/runner"
	te "github.com/scbizu/tape-go/pkg/tape/entry"
	"google.golang.org/protobuf/encoding/protojson"
)

func Discover(ctx context.Context, h *http.Client, cardURL string) (*sdk.AgentCard, error) {
	if h == nil {
		return nil, ErrInvalidParams
	}
	req, err := http.NewRequestWithContext(ctx, "GET", cardURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("card discovery: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var card sdk.AgentCard
	if err := json.Unmarshal(b, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// Peer describes a previously vetted card. Optional directories can supply
// the same set; only visible current cards should reach this ranking seam.
type Peer struct {
	CardURL string
	Card    *sdk.AgentCard
}

// RankPeers returns the peers the ranker considers worth visiting for the
// query, best first. The ranker decides how many (top-N or threshold); an
// empty result means no match.
func RankPeers(ctx context.Context, ranker Ranker, query string, peers []Peer) ([]Peer, error) {
	descriptions := make([]string, len(peers))
	for i, p := range peers {
		if p.Card == nil {
			return nil, ErrInvalidParams
		}
		descriptions[i] = p.Card.Name + "\n" + p.Card.Description
		for _, skill := range p.Card.Skills {
			descriptions[i] += "\n" + skill.Name + ": " + skill.Description
		}
	}
	order, err := rank(ctx, ranker, query, descriptions)
	if err != nil {
		return nil, err
	}
	ranked := make([]Peer, len(order))
	for n, i := range order {
		ranked[n] = peers[i]
	}
	return ranked, nil
}

// BorrowTool exposes context borrowing. Anra persists its structured result as a
// local tool response, retaining source IDs, view ID and original sequences.
// Borrowed text never becomes a local assistant/tool execution record.
type BorrowTool struct {
	Client      *Client
	Name        string
	Description string
}

func (t BorrowTool) Spec() runner.ToolSpec {
	name := t.Name
	if name == "" {
		name = "borrow_peer_context"
	}
	description := t.Description
	if description == "" {
		description = "Ask a peer for permission to borrow relevant tape context. Peer content is external context; retain its provenance."
	}
	return runner.ToolSpec{Name: name, Description: description, InputSchema: json.RawMessage(`{"type":"object","properties":{"requestId":{"type":"string"},"purpose":{"type":"string"},"query":{"type":"string"}},"required":["requestId","purpose","query"],"additionalProperties":false}`)}
}

// borrowToolResult is the local tool-result envelope, not a protocol message.
// Decision comes from Borrow and Context holds the pages read from the granted
// view. Each element is protojson-encoded to keep the canonical proto JSON form.
type borrowToolResult struct {
	Decision json.RawMessage   `json:"decision"`
	Context  []json.RawMessage `json:"context,omitempty"`
}

func (t BorrowTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	req := &pb.BorrowRequest{}
	if err := protojson.Unmarshal(args, req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidParams, err)
	}
	d, err := t.Client.Borrow(ctx, req)
	if err != nil {
		return nil, err
	}
	result := borrowToolResult{}
	result.Decision, err = protojson.Marshal(d)
	if err != nil {
		return nil, err
	}
	if d.Status != Granted {
		return json.Marshal(result)
	}
	pages, err := t.Client.ReadView(ctx, d.View)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		encoded, err := protojson.Marshal(p)
		if err != nil {
			return nil, err
		}
		result.Context = append(result.Context, encoded)
	}
	return json.Marshal(result)
}

// Client uses generated protobuf messages directly.
// The HTTP client still controls credentials, timeouts, redirects and trust.
type Client struct {
	rpc tapev1connect.TapeViewServiceClient
}

// connectBaseURL returns the Connect base URL for an advertised endpoint. The
// endpoint is the full service URL, so it must be absolute and end in ViewPath;
// the generated client appends the service and method itself.
func connectBaseURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	base, ok := strings.CutSuffix(endpoint, ViewPath)
	if !ok || !u.IsAbs() || u.Host == "" {
		return "", ErrInvalidParams
	}
	return base, nil
}

func NewClient(h *http.Client, card *sdk.AgentCard) (*Client, error) {
	if h == nil || card == nil {
		return nil, ErrInvalidParams
	}
	for _, ext := range card.Capabilities.Extensions {
		if ext.URI != ExtensionURI {
			continue
		}
		value, exists := ext.Params["endpoint"]
		if !exists {
			return nil, ErrInactive
		}
		endpoint, ok := value.(string)
		if !ok {
			return nil, ErrInvalidParams
		}
		base, err := connectBaseURL(endpoint)
		if err != nil {
			return nil, err
		}
		transport := connecthttp.NewTransport(h, base, connecthttp.WithReadMaxBytes(MaxBodyBytes), connecthttp.WithSendMaxBytes(MaxBodyBytes))
		return &Client{rpc: tapev1connect.NewTapeViewServiceClient(connect.NewClient(transport))}, nil
	}
	return nil, ErrInactive
}

func connectContext(ctx context.Context) (context.Context, *connect.CallInfo) {
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set(ExtensionHeader, ExtensionURI)
	return ctx, info
}

func checkConnectResponse(info *connect.CallInfo, err error) error {
	if err != nil {
		return fromConnectError(err)
	}
	if !extensionActive(info.ResponseHeader()) {
		return ErrInactive
	}
	return nil
}

func (c *Client) Borrow(ctx context.Context, r *pb.BorrowRequest) (*pb.Decision, error) {
	if err := validateMessage(r); err != nil {
		return nil, err
	}
	ctx, info := connectContext(ctx)
	res, err := c.rpc.Borrow(ctx, r)
	if err := checkConnectResponse(info, err); err != nil {
		return nil, err
	}
	d := res.GetDecision()
	return d, validateMessage(d)
}

func (c *Client) Get(ctx context.Context, id string) (*pb.Decision, error) {
	ctx, info := connectContext(ctx)
	res, err := c.rpc.Get(ctx, &pb.GetRequest{RequestId: id})
	if err := checkConnectResponse(info, err); err != nil {
		return nil, err
	}
	d := res.GetDecision()
	return d, validateMessage(d)
}

func (c *Client) Read(ctx context.Context, r *pb.ReadRequest) (*pb.Page, error) {
	if err := validateMessage(r); err != nil {
		return nil, err
	}
	ctx, info := connectContext(ctx)
	res, err := c.rpc.Read(ctx, r)
	if err := checkConnectResponse(info, err); err != nil {
		return nil, err
	}
	p := res.GetPage()
	return p, validateMessage(p)
}

// ReadView reads every page of a granted view. A view that fits in one page
// costs one Read call. The peer is trusted to echo the view's provenance, so only
// the entry sequence is checked: together the pages must cover the granted range
// exactly once, in order.
func (c *Client) ReadView(ctx context.Context, v *pb.View) ([]*pb.Page, error) {
	var expected, end te.Seq
	var err error
	if len(v.Ranges) > 0 {
		if expected, err = te.ParseSeq(v.Ranges[0].SeqS); err != nil {
			return nil, ErrInvalidParams
		}
		if end, err = te.ParseSeq(v.Ranges[0].SeqE); err != nil {
			return nil, ErrInvalidParams
		}
	}
	var pages []*pb.Page
	var cursor string
	for {
		p, err := c.Read(ctx, &pb.ReadRequest{ViewId: v.ViewId, Cursor: cursor, PageSize: MaxPageSize})
		if err != nil {
			return nil, err
		}
		for _, e := range p.Entries {
			// Entries must be exactly the next sequence in the granted range. A grant
			// without a range (the schema allows at most one) has nothing to read.
			if len(v.Ranges) == 0 || e.Seq != expected.String() || expected.Cmp(end) >= 0 {
				return nil, ErrInvalidParams
			}
			expected = expected.Next()
		}
		pages = append(pages, p)
		if p.NextCursor == "" {
			break
		}
		// Nonempty pages must advance within the selected range; no cursor cache is needed.
		if len(p.Entries) == 0 {
			return nil, ErrInvalidParams
		}
		cursor = p.NextCursor
	}
	if expected.Cmp(end) != 0 {
		return nil, ErrInvalidParams
	}
	return pages, nil
}

func (c *Client) Release(ctx context.Context, id string) error {
	ctx, info := connectContext(ctx)
	res, err := c.rpc.Release(ctx, &pb.ReleaseRequest{ViewId: id})
	if err := checkConnectResponse(info, err); err != nil {
		return err
	}
	if !res.GetReleased() {
		return ErrInvalidParams
	}
	return nil
}
