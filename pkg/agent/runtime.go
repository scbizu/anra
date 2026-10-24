package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/scbizu/anra/pkg/a2a"
	"github.com/scbizu/anra/pkg/discovery"
	tapeagent "github.com/scbizu/tape-go/pkg/agent"
	"github.com/scbizu/tape-go/pkg/tape"
	"github.com/scbizu/tape-go/pkg/tape/owner"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	adkrunner "google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

const defaultMaxTurns = 32
const appName = "anra"

var (
	ErrRuntimeNotConfigured = errors.New("agent: runtime is not configured")
	ErrRuntimeTurnLimit     = errors.New("agent: runtime turn limit reached")
)

type runtimeConfig struct {
	llm               model.LLM
	systemInstruction string
	directory         *discovery.Client
	peerHTTP          *http.Client
	borrowClient      *a2a.Client
	maxTurns          int
	adapter           *tapeagent.TapeAdapter
	runner            *adkrunner.Runner
	sessionID         string
}

type Option func(*Anra) error

func WithLLM(value model.LLM) Option {
	return func(a *Anra) error {
		if value == nil {
			return errors.New("agent: nil model")
		}
		a.runtime.llm = value
		return nil
	}
}

func WithSystemInstruction(value string) Option {
	return func(a *Anra) error { a.runtime.systemInstruction = value; return nil }
}

func WithMaxTurns(limit int) Option {
	return func(a *Anra) error {
		if limit <= 0 {
			return errors.New("agent: max turns must be positive")
		}
		a.runtime.maxTurns = limit
		return nil
	}
}

type turnCountKey struct{}

func (a *Anra) initialize() error {
	ctx := owner.WithOwnerId(context.Background(), a.principal)
	state, err := a.tape.Get(ctx)
	if err != nil {
		return err
	}
	a.runtime.sessionID = state.SessionID
	wrapped := &eventStorage{TapeStorage: a.tape}
	adapter, err := tapeagent.NewTapeAdapter(&tape.Tape{TapeStorage: wrapped, OwnerID: a.principal}, appName)
	if err != nil {
		return err
	}
	a.runtime.adapter = adapter
	if a.runtime.llm == nil {
		return nil
	}
	tools, err := a.builtinTools()
	if err != nil {
		return err
	}
	runtime, err := tapeagent.NewRuntime(llmagent.Config{
		Name:                appName,
		Description:         "Tape-backed Anra agent",
		Model:               a.runtime.llm,
		InstructionProvider: func(adkagent.ReadonlyContext) (string, error) { return a.runtime.systemInstruction, nil },
		Tools:               tools,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{
			func(ctx adkagent.CallbackContext, _ *model.LLMRequest) (*model.LLMResponse, error) {
				count, ok := ctx.Value(turnCountKey{}).(*int)
				if !ok {
					return nil, errors.New("agent: missing invocation turn counter")
				}
				if *count >= a.runtime.maxTurns {
					return nil, ErrRuntimeTurnLimit
				}
				(*count)++
				return nil, nil
			},
			adapter.ContextWindow,
		},
	})
	if err != nil {
		return err
	}
	a.runtime.runner, err = adkrunner.New(adkrunner.Config{
		AppName: appName, Agent: runtime, SessionService: adapter, MemoryService: adapter,
	})
	return err
}

// Run delegates the model/tool loop and event persistence to ADK and tape-go.
func (a *Anra) Run(ctx context.Context, input string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtime.runner == nil {
		return "", ErrRuntimeNotConfigured
	}
	if strings.TrimSpace(input) == "" {
		return "", errors.New("agent: empty runtime input")
	}
	count := 0
	ctx = context.WithValue(ctx, turnCountKey{}, &count)
	var answer string
	for event, err := range a.runtime.runner.Run(ctx, a.principal, a.runtime.sessionID, genai.NewContentFromText(input, genai.RoleUser), adkagent.RunConfig{}) {
		if err != nil {
			return "", fmt.Errorf("agent: ADK run: %w", err)
		}
		if event.ErrorCode != "" || event.ErrorMessage != "" {
			return "", fmt.Errorf("agent: model error %s: %s", event.ErrorCode, event.ErrorMessage)
		}
		if event.IsFinalResponse() && event.Content != nil {
			var text strings.Builder
			for _, part := range event.Content.Parts {
				if part != nil && !part.Thought {
					text.WriteString(part.Text)
				}
			}
			if text.Len() > 0 {
				answer = text.String()
			}
		}
	}
	if answer == "" {
		return "", errors.New("agent: ADK run returned no answer")
	}
	return answer, nil
}

// Exchange records an explicit transport exchange without invoking a local model.
// The network call does not hold the runtime lock, allowing concurrent reception.
func (a *Anra) Exchange(ctx context.Context, input string, respond func(context.Context, string) (string, error)) (string, error) {
	if strings.TrimSpace(input) == "" || respond == nil {
		return "", errors.New("agent: exchange requires a message and responder")
	}
	a.mu.Lock()
	err := a.appendContent(ctx, genai.NewContentFromText(input, genai.RoleUser))
	a.mu.Unlock()
	if err != nil {
		return "", err
	}
	answer, err := respond(ctx, input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(answer) == "" {
		return "", errors.New("agent: empty peer answer")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.appendContent(ctx, genai.NewContentFromText(answer, genai.RoleModel)); err != nil {
		return "", err
	}
	return answer, nil
}

func (a *Anra) appendContent(ctx context.Context, content *genai.Content) error {
	stored, err := a.runtime.adapter.Get(ctx, &session.GetRequest{AppName: appName, UserID: a.principal, SessionID: a.runtime.sessionID})
	if err != nil {
		return err
	}
	event := session.NewEvent("")
	event.Author = appName
	if content.Role == genai.RoleUser {
		event.Author = "user"
	}
	event.Content = content
	return a.runtime.adapter.AppendEvent(ctx, stored.Session, event)
}
