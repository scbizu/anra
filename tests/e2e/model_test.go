package e2e_test

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/scbizu/tape-go/pkg/runner"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// modelFunc is a deterministic ADK model fixture. The neutral request shape is
// used only by these scenarios to inspect the observable conversation contract.
func (f modelFunc) Name() string { return "e2e-model" }
func (f modelFunc) GenerateContent(ctx context.Context, request *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		r := runner.Request{Model: request.Model}
		if request.Config != nil {
			if request.Config.SystemInstruction != nil {
				for _, p := range request.Config.SystemInstruction.Parts {
					r.SystemInstruction += p.Text
				}
			}
			for _, tools := range request.Config.Tools {
				for _, decl := range tools.FunctionDeclarations {
					schema, _ := json.Marshal(decl.ParametersJsonSchema)
					r.Tools = append(r.Tools, runner.ToolSpec{Name: decl.Name, Description: decl.Description, InputSchema: schema})
				}
			}
		}
		for _, content := range request.Contents {
			message := runner.Message{Role: runner.RoleAssistant}
			if content.Role == genai.RoleUser {
				message.Role = runner.RoleUser
			}
			for _, p := range content.Parts {
				message.Text += p.Text
				if c := p.FunctionCall; c != nil {
					args, _ := json.Marshal(c.Args)
					message.ToolCalls = append(message.ToolCalls, runner.ToolCall{ID: c.ID, Name: c.Name, Arguments: args})
				}
				if result := p.FunctionResponse; result != nil {
					payload, _ := json.Marshal(result.Response)
					message.Role = runner.RoleTool
					message.ToolResults = append(message.ToolResults, runner.ToolResult{ID: result.ID, Name: result.Name, Result: payload})
				}
			}
			r.Messages = append(r.Messages, message)
		}
		response, err := f(ctx, r)
		if err != nil {
			yield(nil, err)
			return
		}
		content := &genai.Content{Role: genai.RoleModel}
		if response.Output != "" {
			content.Parts = append(content.Parts, &genai.Part{Text: response.Output})
		}
		for _, call := range response.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal(call.Arguments, &args); err != nil {
				yield(nil, err)
				return
			}
			content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: call.ID, Name: call.Name, Args: args}})
		}
		yield(&model.LLMResponse{Content: content, TurnComplete: true}, nil)
	}
}

type completionMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// newModelServer speaks the real provider HTTP protocol. No fixture provider or
// mock runtime is exposed by the production binary.
func newModelServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer e2e-model-key" {
			http.Error(w, "invalid fixture key", http.StatusUnauthorized)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []completionMessage `json:"messages"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		identity := "unknown"
		input := ""
		for _, message := range request.Messages {
			if message.Role == "system" {
				if _, name, ok := strings.Cut(message.Content, "You are Anra agent "); ok {
					identity = strings.SplitN(name, ".\n", 2)[0]
					identity = strings.TrimSuffix(identity, ".")
				}
			}
			if message.Role == "user" {
				input = message.Content
			}
		}
		message := map[string]any{"role": "assistant", "content": identity + ": " + input}
		finish := "stop"
		last := request.Messages[len(request.Messages)-1]
		if input == "recall the peer answer" {
			message["content"] = "no peer answer recorded"
			for _, prior := range request.Messages {
				if prior.Role == "tool" {
					var result struct {
						Peer   string `json:"peer"`
						Answer string `json:"answer"`
					}
					if json.Unmarshal([]byte(prior.Content), &result) == nil && result.Peer == "bob" && result.Answer != "" {
						message["content"] = result.Answer
					}
				}
			}
		}
		if input == "run the built-in bash tool" {
			if last.Role == "tool" {
				var result struct {
					Output string `json:"output"`
				}
				_ = json.Unmarshal([]byte(last.Content), &result)
				message["content"] = result.Output
			} else {
				allowed := false
				for _, candidate := range request.Tools {
					if candidate.Function.Name == "bash" {
						allowed = true
					}
				}
				if allowed {
					message["content"] = nil
					message["tool_calls"] = []any{map[string]any{"id": "fixture-bash", "type": "function", "function": map[string]any{"name": "bash", "arguments": `{"command":"printf anra-bash-test"}`}}}
					finish = "tool_calls"
				} else {
					message["content"] = "tool unavailable: bash"
				}
			}
		}
		// Scripted delegation exercises native discovery and messaging tools.
		if input == "discover peers and ask bob to say hello" {
			next := "discover_peers"
			args := "{}"
			if last.Role == "tool" {
				if strings.Contains(last.Content, `"agents"`) {
					next = "send_peer_message"
					args = `{"peer":"bob","message":"hello from alice"}`
				} else {
					var result struct {
						Answer string `json:"answer"`
					}
					_ = json.Unmarshal([]byte(last.Content), &result)
					message["content"] = result.Answer
					next = ""
				}
			}
			if next != "" {
				allowed := false
				for _, candidate := range request.Tools {
					if candidate.Function.Name == next {
						allowed = true
					}
				}
				if !allowed {
					message["content"] = "tool unavailable: " + next
				} else {
					message["content"] = nil
					message["tool_calls"] = []any{map[string]any{"id": "fixture-" + next, "type": "function", "function": map[string]any{"name": next, "arguments": args}}}
					finish = "tool_calls"
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "e2e-completion", "object": "chat.completion", "created": 1, "model": "e2e-model", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
	}))
}

var _ model.LLM = modelFunc(nil)
