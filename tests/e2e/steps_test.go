package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/cucumber/godog"
	pb "github.com/scbizu/anra/gen/anra/a2a/tape/v1"
	a2a "github.com/scbizu/anra/pkg/a2a"
	"github.com/scbizu/anra/pkg/agent"
	"github.com/scbizu/tape-go/pkg/runner"
	entry "github.com/scbizu/tape-go/pkg/tape/entry"
	"github.com/scbizu/tape-go/pkg/tape/owner"
	"github.com/scbizu/tape-go/pkg/tape/storage/bbolt"
	"github.com/scbizu/tape-go/pkg/tape/view"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Only the model and permission decision are deterministic fixtures. Agent,
// tape, journal, discovery, SDK, Connect, and HTTP all use real implementations.
type scenario struct {
	root                string
	ctx                 context.Context
	cancel              context.CancelFunc
	localTape, peerTape *bbolt.Bbolt
	service             *a2a.Service
	server              *httptest.Server
	modelServer         *httptest.Server
	card                *sdk.AgentCard
	client              *a2a.Client
	ordinary            *a2aclient.Client
	request             *pb.BorrowRequest
	decision            *pb.Decision
	firstPage           *pb.Page
	lastRequest         runner.Request
	answer              string
	lastError           error
	wireCode            string
	pending             atomic.Bool
	findingCount        atomic.Uint64
	clock               atomic.Int64
}

func initializeScenario(sc *godog.ScenarioContext) {
	s := &scenario{}
	initializeCLI(sc, s)
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		root, err := os.MkdirTemp("", "anra-e2e-")
		if err != nil {
			return ctx, err
		}
		*s = scenario{root: root}
		s.modelServer = newModelServer()
		s.ctx, s.cancel = context.WithTimeout(ctx, 20*time.Second)
		s.clock.Store(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC).UnixNano())
		return s.ctx, nil
	})
	sc.After(func(ctx context.Context, sc *godog.Scenario, scenarioErr error) (context.Context, error) {
		err := s.stopPeer()
		if s.modelServer != nil {
			s.modelServer.Close()
		}
		if s.localTape != nil {
			err = errors.Join(err, s.localTape.Close())
		}
		if s.cancel != nil {
			s.cancel()
		}
		if failure := errors.Join(scenarioErr, err); failure != nil && os.Getenv("ANRA_E2E_ARTIFACTS") != "" {
			if saveErr := s.saveFailure(sc.Name, failure); saveErr != nil {
				return ctx, errors.Join(err, saveErr)
			}
		}
		return ctx, errors.Join(err, os.RemoveAll(s.root))
	})
	sc.Step(`^an agent with its own tape$`, s.openLocalTape)
	sc.Step(`^I ask "([^"]*)" and the model answers "([^"]*)"$`, s.ask)
	sc.Step(`^I close and reopen the agent's storage$`, s.reopenLocalTape)
	sc.Step(`^another member uses the same database for a private conversation$`, s.otherMember)
	sc.Step(`^the model sees the conversation in order$`, s.conversation)
	sc.Step(`^the answer is "([^"]*)"$`, s.answerIs)
	sc.Step(`^a researcher peer with two findings and a durable grant journal$`, s.setupPeer)
	sc.Step(`^the borrower discovers the researcher and sends "([^"]*)"$`, s.sendMessage)
	sc.Step(`^the peer answers "([^"]*)"$`, s.answerIs)
	sc.Step(`^the peer can stream a response to "([^"]*)"$`, s.streamMessage)
	sc.Step(`^the borrower asks its agent to use the researcher's findings$`, s.borrowWithAgent)
	sc.Step(`^the borrowed tool result retains the source and sequence provenance$`, s.checkBorrowedContext)
	sc.Step(`^I close and reopen the borrower agent's storage$`, s.reopenLocalTape)
	sc.Step(`^its model can recall the findings as a tool result$`, s.recallBorrowedContext)
	sc.Step(`^every stored borrower entry belongs to the borrower$`, s.checkLocalOwnership)
	sc.Step(`^the borrower requests context$`, s.borrow)
	sc.Step(`^the decision is "([^"]*)"$`, s.decisionIs)
	sc.Step(`^the borrower reads the first finding$`, s.readFirst)
	sc.Step(`^the researcher adds a finding after the grant$`, func() error { return s.appendFinding("new finding outside the snapshot") })
	sc.Step(`^the researcher closes and reopens its storage and server$`, s.restartPeer)
	sc.Step(`^retrying the request returns the same grant$`, s.retryGrant)
	sc.Step(`^the borrower can finish paging the original snapshot$`, s.finishSnapshot)
	sc.Step(`^the researcher has (\d+) findings$`, s.addFindings)
	sc.Step(`^the borrower receives all (\d+) findings across multiple pages$`, s.readAll)
	sc.Step(`^the borrower changes the query but keeps the request ID$`, s.conflictingBorrow)
	sc.Step(`^the request fails with a conflict$`, func() error { return expectError(s.lastError, a2a.ErrConflict) })
	sc.Step(`^another peer cannot look up, read, or release that grant$`, s.checkIntruder)
	sc.Step(`^another peer's own request is denied without context$`, s.checkDenied)
	sc.Step(`^the grant is (released|revoked|expired)$`, s.closeGrant)
	sc.Step(`^the borrower cannot read the grant$`, s.unavailable)
	sc.Step(`^the recorded decision still identifies the original grant$`, s.recoverGrant)
	sc.Step(`^the researcher requires approval for borrowing$`, func() { s.pending.Store(true) })
	sc.Step(`^the decision is still "([^"]*)"$`, s.recoverDecision)
	sc.Step(`^the researcher resolves the request as "([^"]*)"$`, s.resolve)
	sc.Step(`^access matches the "([^"]*)" decision$`, s.checkAccess)
	sc.Step(`^a peer submits a wire request with "([^"]*)"$`, s.wireRequest)
	sc.Step(`^the wire error is "([^"]*)"$`, func(want string) error {
		if s.wireCode != want {
			return fmt.Errorf("wire error = %q, want %q", s.wireCode, want)
		}
		return nil
	})
}

// Called after closing the databases so the saved files can be reopened safely.
func (s *scenario) saveFailure(name string, failure error) error {
	parent := os.Getenv("ANRA_E2E_ARTIFACTS")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("save failure artifacts (original files at %s): %w", s.root, err)
	}
	dir, err := os.MkdirTemp(parent, "scenario-")
	if err == nil {
		err = os.CopyFS(dir, os.DirFS(s.root))
	}
	if err != nil {
		return fmt.Errorf("save failure artifacts (original files at %s): %w", s.root, err)
	}
	data, err := json.MarshalIndent(map[string]any{
		"scenario": name, "error": failure.Error(), "answer": s.answer,
		"model_request": s.lastRequest, "borrow_request": s.request,
		"decision": s.decision, "first_page": s.firstPage,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "failure.json"), data, 0o644)
}

type modelFunc func(context.Context, runner.Request) (runner.Response, error)

func (s *scenario) openTape(name, principal string) (*bbolt.Bbolt, error) {
	tape, err := bbolt.NewBboltStorage("session", filepath.Join(s.root, name+".db"))
	if err != nil {
		return nil, err
	}
	if err := tape.Init(owner.WithOwnerId(s.ctx, principal)); err != nil {
		return nil, errors.Join(err, tape.Close())
	}
	return tape, nil
}

func (s *scenario) openLocalTape() error {
	var err error
	s.localTape, err = s.openTape("borrower", "borrower-local")
	return err
}

func (s *scenario) reopenLocalTape() error {
	if err := s.localTape.Close(); err != nil {
		return err
	}
	s.localTape = nil
	return s.openLocalTape()
}

func (s *scenario) ask(question, answer string) error {
	a, err := agent.New("borrower-local", s.localTape, agent.WithLLM(modelFunc(func(_ context.Context, req runner.Request) (runner.Response, error) {
		s.lastRequest = req
		return runner.Response{Output: answer}, nil
	})))
	if err != nil {
		return err
	}
	s.answer, err = a.Run(s.ctx, question)
	return err
}

func (s *scenario) otherMember() error {
	if err := s.localTape.Init(owner.WithOwnerId(s.ctx, "other-local")); err != nil {
		return err
	}
	a, err := agent.New("other-local", s.localTape, agent.WithLLM(modelFunc(func(_ context.Context, req runner.Request) (runner.Response, error) {
		if len(req.Messages) != 1 || req.Messages[0].Text != "private question" {
			return runner.Response{}, fmt.Errorf("another member received private history: %+v", req.Messages)
		}
		return runner.Response{Output: "private answer"}, nil
	})))
	if err != nil {
		return err
	}
	_, err = a.Run(s.ctx, "private question")
	return err
}

func (s *scenario) conversation(table *godog.Table) error {
	rows := table.Rows[1:]
	if len(rows) != len(s.lastRequest.Messages) {
		return fmt.Errorf("conversation has %d messages, want %d", len(s.lastRequest.Messages), len(rows))
	}
	for i, row := range rows {
		got := s.lastRequest.Messages[i]
		if string(got.Role) != row.Cells[0].Value || got.Text != row.Cells[1].Value {
			return fmt.Errorf("message %d = %+v, want %s: %s", i, got, row.Cells[0].Value, row.Cells[1].Value)
		}
	}
	return nil
}

func (s *scenario) answerIs(want string) error {
	if s.answer != want {
		return fmt.Errorf("answer = %q, want %q", s.answer, want)
	}
	return nil
}

func (s *scenario) setupPeer() error {
	s.request = &pb.BorrowRequest{RequestId: "research-request", Purpose: "fix storage concurrency", Query: "bbolt"}
	var err error
	s.peerTape, err = s.openTape("researcher", "researcher-local")
	if err != nil {
		return err
	}
	for _, finding := range []string{"bbolt requires a single writer", "concurrent updates need version checks"} {
		if err := s.appendFinding(finding); err != nil {
			return err
		}
	}
	return s.startPeer()
}

func (s *scenario) appendFinding(text string) error {
	_, err := s.peerTape.Store(owner.WithOwnerId(s.ctx, "researcher-local"), entry.NewEntry(
		entry.WithEntryKind(entry.EntryUser), entry.WithEntryOwner("researcher-local"), entry.WithEntryContent(text)))
	if err == nil {
		s.findingCount.Add(1)
	}
	return err
}

func (s *scenario) selection() a2a.Selection {
	return a2a.Selection{Status: a2a.Granted, Range: &view.EntryRange{SeqS: entry.SeqFromUint64(1), SeqE: entry.SeqFromUint64(s.findingCount.Load() + 1)}, TTL: time.Hour}
}

func (s *scenario) startPeer() error {
	var err error
	s.service, err = a2a.NewService(a2a.Config{
		Source: &pb.Source{AgentId: "researcher", TapeId: "researcher-incarnation"},
		Owner:  "researcher-local", Tape: s.peerTape, JournalPath: filepath.Join(s.root, "grants.db"),
		Now: func() time.Time { return time.Unix(0, s.clock.Load()) },
		Policy: a2a.PolicyFunc(func(_ context.Context, peer string, _ *pb.BorrowRequest, _ entry.Seq) (a2a.Selection, error) {
			if peer != "borrower" {
				return a2a.Selection{Status: a2a.Denied}, nil
			}
			if s.pending.Load() {
				return a2a.Selection{Status: a2a.Pending}, nil
			}
			return s.selection(), nil
		}),
	})
	if err != nil {
		return err
	}
	lender, err := agent.New("researcher-local", s.peerTape, agent.WithLLM(modelFunc(func(context.Context, runner.Request) (runner.Response, error) {
		return runner.Response{Output: "research response"}, nil
	})))
	if err != nil {
		return err
	}
	executor, err := a2a.NewExecutor(lender)
	if err != nil {
		return err
	}
	s.server = httptest.NewUnstartedServer(nil)
	base := "http://" + s.server.Listener.Addr().String()
	card := &sdk.AgentCard{Name: "researcher", Description: "storage specialist", Version: "0.1",
		SupportedInterfaces: []*sdk.AgentInterface{sdk.NewAgentInterface(base+a2a.CorePath, sdk.TransportProtocolJSONRPC)},
		DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
		Skills: []sdk.AgentSkill{{ID: "storage", Name: "Storage", Description: "bbolt storage", Tags: []string{"bbolt"}}},
	}
	if err := a2a.Advertise(card, base+a2a.ViewPath); err != nil {
		return err
	}
	handler, err := a2a.NewPeerHandler(card, a2asrv.NewHandler(executor), s.service, a2a.TrustedPeerIdentity)
	if err != nil {
		return err
	}
	s.server.Config.Handler = handler
	s.server.Start()
	s.card, err = a2a.Discover(s.ctx, s.server.Client(), base+a2asrv.WellKnownAgentCardPath)
	if err != nil {
		return err
	}
	s.client, err = s.clientFor("borrower")
	return err
}

func (s *scenario) stopPeer() error {
	if s.server != nil {
		s.server.Close()
		s.server = nil
	}
	var err error
	if s.service != nil {
		err = s.service.Close()
		s.service = nil
	}
	if s.peerTape != nil {
		err = errors.Join(err, s.peerTape.Close())
		s.peerTape = nil
	}
	return err
}

func (s *scenario) restartPeer() error {
	if err := s.stopPeer(); err != nil {
		return err
	}
	var err error
	s.peerTape, err = s.openTape("researcher", "researcher-local")
	if err != nil {
		return err
	}
	return s.startPeer()
}

func (s *scenario) httpClient(peer string) *http.Client {
	client := a2a.TrustedHTTPClient(peer)
	client.Timeout = 5 * time.Second
	return client
}
func (s *scenario) clientFor(peer string) (*a2a.Client, error) {
	return a2a.NewClient(s.httpClient(peer), s.card)
}

func (s *scenario) sendMessage(text string) error {
	var err error
	s.ordinary, err = a2aclient.NewFromCard(s.ctx, s.card, a2aclient.WithJSONRPCTransport(s.httpClient("borrower")))
	if err != nil {
		return err
	}
	result, err := s.ordinary.SendMessage(s.ctx, &sdk.SendMessageRequest{Message: sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart(text))})
	if err != nil {
		return err
	}
	message, ok := result.(*sdk.Message)
	if !ok || len(message.Parts) != 1 {
		return fmt.Errorf("unexpected peer message: %#v", result)
	}
	s.answer = message.Parts[0].Text()
	return nil
}

func (s *scenario) streamMessage(text string) error {
	var outputs []string
	for event, err := range s.ordinary.SendStreamingMessage(s.ctx, &sdk.SendMessageRequest{Message: sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart(text))}) {
		if err != nil {
			return err
		}
		if message, ok := event.(*sdk.Message); ok {
			for _, part := range message.Parts {
				outputs = append(outputs, part.Text())
			}
		}
	}
	if strings.Join(outputs, "") != "research response" {
		return fmt.Errorf("streamed output = %q", outputs)
	}
	return nil
}

func (s *scenario) borrow() error {
	var err error
	s.decision, err = s.client.Borrow(s.ctx, s.request)
	return err
}
func decisionStatus(name string) pb.DecisionStatus {
	switch name {
	case "granted":
		return a2a.Granted
	case "denied":
		return a2a.Denied
	case "pending":
		return a2a.Pending
	}
	return pb.DecisionStatus_DECISION_STATUS_UNSPECIFIED
}
func (s *scenario) decisionIs(want string) error {
	if s.decision == nil || s.decision.Status != decisionStatus(want) {
		return fmt.Errorf("decision = %v, want %s", s.decision, want)
	}
	if want != "granted" && s.decision.View != nil {
		return fmt.Errorf("%s decision disclosed a view", want)
	}
	return nil
}
func (s *scenario) readFirst() error {
	var err error
	s.firstPage, err = s.client.Read(s.ctx, &pb.ReadRequest{ViewId: s.decision.View.ViewId, PageSize: 1})
	if err != nil {
		return err
	}
	if len(s.firstPage.Entries) != 1 || s.firstPage.Entries[0].Summary != "bbolt requires a single writer" || s.firstPage.NextCursor == "" {
		return fmt.Errorf("unexpected first page: %v", s.firstPage)
	}
	return nil
}
func (s *scenario) retryGrant() error {
	got, err := s.client.Borrow(s.ctx, s.request)
	if err != nil {
		return err
	}
	if !proto.Equal(got, s.decision) {
		return fmt.Errorf("retry changed decision: got %v, want %v", got, s.decision)
	}
	return nil
}
func (s *scenario) finishSnapshot() error {
	page, err := s.client.Read(s.ctx, &pb.ReadRequest{ViewId: s.decision.View.ViewId, Cursor: s.firstPage.NextCursor, PageSize: 1})
	if err != nil {
		return err
	}
	if len(page.Entries) != 1 || page.Entries[0].Summary != "concurrent updates need version checks" || page.Entries[0].Seq != "2" || page.NextCursor != "" || page.SnapshotHead != s.firstPage.SnapshotHead || !proto.Equal(page.Source, s.firstPage.Source) {
		return fmt.Errorf("original snapshot was not preserved: %v", page)
	}
	return nil
}
func (s *scenario) addFindings(count int) error {
	for int(s.findingCount.Load()) < count {
		if err := s.appendFinding(fmt.Sprintf("finding %d", s.findingCount.Load()+1)); err != nil {
			return err
		}
	}
	return nil
}
func (s *scenario) readAll(count int) error {
	pages, err := s.client.ReadView(s.ctx, s.decision.View)
	if err != nil {
		return err
	}
	if len(pages) < 2 {
		return fmt.Errorf("expected multiple pages, got %d", len(pages))
	}
	seen := 0
	for _, page := range pages {
		if page.ViewId != s.decision.View.ViewId || page.SnapshotHead != s.decision.View.SnapshotHead || !proto.Equal(page.Source, s.decision.View.Source) {
			return fmt.Errorf("page lost provenance: %v", page)
		}
		for _, e := range page.Entries {
			seen++
			if e.Seq != fmt.Sprint(seen) {
				return fmt.Errorf("entry %d has sequence %s", seen, e.Seq)
			}
		}
	}
	if seen != count {
		return fmt.Errorf("received %d findings, want %d", seen, count)
	}
	return nil
}
func (s *scenario) conflictingBorrow() {
	changed := proto.Clone(s.request).(*pb.BorrowRequest)
	changed.Query = "a different query"
	_, s.lastError = s.client.Borrow(s.ctx, changed)
}
func expectError(got, want error) error {
	if !errors.Is(got, want) {
		return fmt.Errorf("error = %v, want %v", got, want)
	}
	return nil
}
func (s *scenario) checkIntruder() error {
	intruder, err := s.clientFor("intruder")
	if err != nil {
		return err
	}
	_, err = intruder.Get(s.ctx, s.request.RequestId)
	if err := expectError(err, a2a.ErrUnavailable); err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	_, err = intruder.Read(s.ctx, &pb.ReadRequest{ViewId: s.decision.View.ViewId})
	if err := expectError(err, a2a.ErrUnavailable); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if err := expectError(intruder.Release(s.ctx, s.decision.View.ViewId), a2a.ErrUnavailable); err != nil {
		return fmt.Errorf("release: %w", err)
	}
	// Failed release must leave the authorized borrower's access intact.
	_, err = s.client.ReadView(s.ctx, s.decision.View)
	return err
}
func (s *scenario) checkDenied() error {
	intruder, err := s.clientFor("intruder")
	if err != nil {
		return err
	}
	d, err := intruder.Borrow(s.ctx, s.request)
	if err != nil {
		return err
	}
	if d.Status != a2a.Denied || d.View != nil {
		return fmt.Errorf("unauthorized decision = %v", d)
	}
	return nil
}
func (s *scenario) closeGrant(closure string) error {
	switch closure {
	case "released":
		if err := s.client.Release(s.ctx, s.decision.View.ViewId); err != nil {
			return err
		}
		return s.client.Release(s.ctx, s.decision.View.ViewId)
	case "revoked":
		return s.service.Revoke(s.ctx, "borrower", s.decision.View.ViewId)
	case "expired":
		s.clock.Add(int64(2 * time.Hour))
		return nil
	}
	return fmt.Errorf("unknown closure %s", closure)
}
func (s *scenario) unavailable() error {
	_, err := s.client.Read(s.ctx, &pb.ReadRequest{ViewId: s.decision.View.ViewId})
	return expectError(err, a2a.ErrUnavailable)
}
func (s *scenario) recoverGrant() error {
	d, err := s.client.Get(s.ctx, s.request.RequestId)
	if err != nil {
		return err
	}
	if !proto.Equal(d, s.decision) {
		return fmt.Errorf("recorded grant changed: %v", d)
	}
	return s.retryGrant()
}
func (s *scenario) recoverDecision(want string) error {
	var err error
	s.decision, err = s.client.Get(s.ctx, s.request.RequestId)
	if err != nil {
		return err
	}
	if err := s.decisionIs(want); err != nil {
		return err
	}
	return s.retryGrant()
}
func (s *scenario) resolve(outcome string) error {
	sel := a2a.Selection{Status: a2a.Denied}
	if outcome == "granted" {
		sel = s.selection()
	}
	_, err := s.service.Resolve(s.ctx, "borrower", s.request.RequestId, sel)
	return err
}
func (s *scenario) checkAccess(outcome string) error {
	if outcome == "denied" {
		return s.decisionIs(outcome)
	}
	pages, err := s.client.ReadView(s.ctx, s.decision.View)
	if err != nil {
		return err
	}
	if len(pages) != 1 || len(pages[0].Entries) != 2 {
		return fmt.Errorf("approved context missing: %v", pages)
	}
	return nil
}

func (s *scenario) borrowWithAgent() error {
	if err := s.openLocalTape(); err != nil {
		return err
	}
	turns := 0
	a, err := agent.New("borrower-local", s.localTape, agent.WithBorrowClient(s.client), agent.WithLLM(modelFunc(func(_ context.Context, req runner.Request) (runner.Response, error) {
		turns++
		if turns == 1 {
			return runner.Response{ToolCalls: []runner.ToolCall{{ID: "borrow-call", Name: "borrow_peer_context", Arguments: json.RawMessage(`{"requestId":"agent-borrow","purpose":"fix storage concurrency","query":"bbolt"}`)}}}, nil
		}
		s.lastRequest = req
		if err := s.checkBorrowedContext(); err != nil {
			return runner.Response{}, err
		}
		return runner.Response{Output: "Use version checks for competing writes."}, nil
	})))
	if err != nil {
		return err
	}
	s.answer, err = a.Run(s.ctx, "help with storage concurrency")
	return err
}
func (s *scenario) checkBorrowedContext() error {
	for _, message := range s.lastRequest.Messages {
		if message.Role != runner.RoleTool {
			continue
		}
		for _, result := range message.ToolResults {
			var envelope struct {
				Decision json.RawMessage   `json:"decision"`
				Context  []json.RawMessage `json:"context"`
			}
			if err := json.Unmarshal(result.Result, &envelope); err != nil {
				return err
			}
			var decision pb.Decision
			if err := protojson.Unmarshal(envelope.Decision, &decision); err != nil {
				return err
			}
			if decision.Status != a2a.Granted || decision.View == nil || len(envelope.Context) != 1 {
				return fmt.Errorf("borrowed result lacks a grant and context: %s", result.Result)
			}
			var page pb.Page
			if err := protojson.Unmarshal(envelope.Context[0], &page); err != nil {
				return err
			}
			if page.Source.GetAgentId() != "researcher" || page.Source.GetTapeId() != "researcher-incarnation" || page.ViewId != decision.View.ViewId || page.SnapshotHead != "2" || len(page.Entries) != 2 {
				return fmt.Errorf("borrowed context lost provenance: %v", &page)
			}
			for i, text := range []string{"bbolt requires a single writer", "concurrent updates need version checks"} {
				if page.Entries[i].Seq != fmt.Sprint(i+1) || page.Entries[i].Summary != text {
					return fmt.Errorf("unexpected borrowed finding: %v", page.Entries[i])
				}
			}
			return nil
		}
	}
	return errors.New("borrowed context missing from tool results")
}
func (s *scenario) recallBorrowedContext() error {
	a, err := agent.New("borrower-local", s.localTape, agent.WithLLM(modelFunc(func(_ context.Context, req runner.Request) (runner.Response, error) {
		s.lastRequest = req
		if err := s.checkBorrowedContext(); err != nil {
			return runner.Response{}, err
		}
		return runner.Response{Output: "memory retained"}, nil
	})))
	if err != nil {
		return err
	}
	s.answer, err = a.Run(s.ctx, "recall the researcher's findings")
	if err != nil {
		return err
	}
	return s.answerIs("memory retained")
}
func (s *scenario) checkLocalOwnership() error {
	ctx := owner.WithOwnerId(s.ctx, "borrower-local")
	head, err := s.localTape.Get(ctx)
	if err != nil {
		return err
	}
	stored, err := s.localTape.Range(ctx, view.EntryRange{SeqS: entry.SeqFromUint64(1), SeqE: head.Scope.SeqE.Next()})
	if err != nil {
		return err
	}
	if len(stored.Raw) != 6 {
		return fmt.Errorf("borrower has %d entries, want 6", len(stored.Raw))
	}
	for _, e := range stored.Raw {
		if e.GetOwner() != "borrower-local" {
			return fmt.Errorf("imported peer ownership: %s", e.GetOwner())
		}
	}
	return nil
}

func (s *scenario) wireRequest(problem string) error {
	body := `{"requestId":"wire-request","purpose":"research","query":"bbolt"}`
	method, identity, extension := "Borrow", "borrower", a2a.ExtensionURI
	switch problem {
	case "no identity":
		identity = ""
	case "no extension":
		extension = ""
	case "missing purpose":
		body = `{"requestId":"wire-request","query":"bbolt"}`
	case "reversed bounds":
		body = `{"requestId":"wire-request","purpose":"research","query":"bbolt","bounds":{"seqS":"9","seqE":"2"}}`
	case "excessive page":
		method, body = "Read", `{"viewId":"unknown","pageSize":101}`
	case "excessive body":
		body = strings.Repeat(" ", a2a.MaxBodyBytes+1)
	default:
		return fmt.Errorf("unknown wire problem %s", problem)
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.server.URL+a2a.ViewPath+"/"+method, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if identity != "" {
		req.Header.Set(a2a.PeerIdentityHeader, identity)
	}
	if extension != "" {
		req.Header.Set(a2a.ExtensionHeader, extension)
	}
	res, err := s.server.Client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var failure struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(data, &failure); err != nil {
		return fmt.Errorf("HTTP %d: %s: %w", res.StatusCode, data, err)
	}
	if res.StatusCode < 400 || failure.Code == "" {
		return fmt.Errorf("request unexpectedly accepted: HTTP %d: %s", res.StatusCode, data)
	}
	s.wireCode = failure.Code
	return nil
}
