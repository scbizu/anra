package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/scbizu/anra/pkg/a2a"
	"github.com/scbizu/anra/pkg/discovery"
	tapeagent "github.com/scbizu/tape-go/pkg/agent"
	tapetools "github.com/scbizu/tape-go/pkg/agent/tools"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// WithPeers configures the network services used by the built-in peer tools.
func WithPeers(directory *discovery.Client, client *http.Client) Option {
	return func(a *Anra) error {
		if directory == nil || client == nil {
			return errors.New("agent: peer services must not be nil")
		}
		a.runtime.directory, a.runtime.peerHTTP = directory, client
		return nil
	}
}

// WithBorrowClient connects the built-in context borrowing tool to its peer.
func WithBorrowClient(client *a2a.Client) Option {
	return func(a *Anra) error {
		if client == nil {
			return errors.New("agent: nil borrow client")
		}
		a.runtime.borrowClient = client
		return nil
	}
}

func (a *Anra) builtinTools() ([]tool.Tool, error) {
	bash, err := tapetools.NewBashTool(tapeagent.NewCommandRegistry(tapeagent.BuiltinBashCommand()))
	if err != nil {
		return nil, err
	}
	peers, err := a2a.PeerTools(a.runtime.directory, a.runtime.peerHTTP, a.principal)
	if err != nil {
		return nil, err
	}
	tools := []tool.Tool{bash, peers["discover_peers"], peers["send_peer_message"]}
	if a.runtime.borrowClient != nil {
		tools = append(tools, borrowTool{a2a.BorrowTool{Client: a.runtime.borrowClient}})
	}
	return tools, nil
}

// borrowTool exposes the built-in context borrowing capability to ADK.
type borrowTool struct{ a2a.BorrowTool }

func (t borrowTool) Name() string        { return t.Spec().Name }
func (t borrowTool) Description() string { return t.Spec().Description }
func (t borrowTool) IsLongRunning() bool { return false }
func (t borrowTool) Declaration() *genai.FunctionDeclaration {
	var schema any
	_ = json.Unmarshal(t.Spec().InputSchema, &schema)
	return &genai.FunctionDeclaration{Name: t.Name(), Description: t.Description(), ParametersJsonSchema: schema}
}
func (t borrowTool) ProcessRequest(_ tool.Context, req *model.LLMRequest) error {
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}
	if _, exists := req.Tools[t.Name()]; exists {
		return fmt.Errorf("duplicate tool %q", t.Name())
	}
	req.Tools[t.Name()] = t
	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	req.Config.Tools = append(req.Config.Tools, &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{t.Declaration()}})
	return nil
}
func (t borrowTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	raw, err := t.Execute(ctx, encoded)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{"error": "tool returned invalid JSON"}, nil
	}
	if object, ok := value.(map[string]any); ok {
		return object, nil
	}
	return map[string]any{"output": value}, nil
}
