package agent

import (
	"context"
	"encoding/json"
	"maps"

	"github.com/scbizu/tape-go/pkg/runner"
	"github.com/scbizu/tape-go/pkg/tape/entry"
	"github.com/scbizu/tape-go/pkg/tape/storage"
	"github.com/scbizu/tape-go/pkg/tape/view"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// eventStorage preserves the tape's tool entry kinds and projects the former
// Anra message payload into ADK events on read. Existing tapes remain append-only.
type eventStorage struct{ storage.TapeStorage }

func (s *eventStorage) Unwrap() storage.TapeStorage { return s.TapeStorage }

func (s *eventStorage) Store(ctx context.Context, raw entry.EntryLike) (entry.EntryLike, error) {
	if value, ok := customEntry(raw); ok {
		if payload, ok := value.Extensions["adk:event"].(string); ok {
			var event session.Event
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				return nil, err
			}
			if event.Content != nil {
				for _, part := range event.Content.Parts {
					if part.FunctionCall != nil {
						value.Entry.Ek = entry.EntryToolCall
					}
					if part.FunctionResponse != nil {
						value.Entry.Ek = entry.EntryToolResult
					}
				}
			}
			raw = value
		}
	}
	return s.TapeStorage.Store(ctx, raw)
}

func (s *eventStorage) Range(ctx context.Context, r view.EntryRange, opts ...storage.RangeBy) (view.EntryView, error) {
	result, err := s.TapeStorage.Range(ctx, r, opts...)
	if err != nil {
		return result, err
	}
	for i, raw := range result.Raw {
		value, ok := customEntry(raw)
		if !ok {
			continue
		}
		if _, exists := value.Extensions["adk:event"]; exists {
			continue
		}
		payload, ok := value.Extensions["anra:runner-message/v1"].(string)
		if !ok {
			continue
		}
		var message runner.Message
		if err := json.Unmarshal([]byte(payload), &message); err != nil {
			return view.EntryView{}, err
		}
		content := &genai.Content{Role: genai.RoleModel}
		author := appName
		if message.Role == runner.RoleUser || message.Role == runner.RoleTool {
			content.Role = genai.RoleUser
		}
		if message.Role == runner.RoleUser {
			author = "user"
		}
		if message.Text != "" {
			content.Parts = append(content.Parts, &genai.Part{Text: message.Text})
		}
		for _, call := range message.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal(call.Arguments, &args); err != nil {
				return view.EntryView{}, err
			}
			content.Parts = append(content.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: call.ID, Name: call.Name, Args: args}})
		}
		for _, toolResult := range message.ToolResults {
			var decoded any
			if err := json.Unmarshal(toolResult.Result, &decoded); err != nil {
				return view.EntryView{}, err
			}
			object, ok := decoded.(map[string]any)
			if !ok {
				object = map[string]any{"output": decoded}
			}
			content.Parts = append(content.Parts, &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: toolResult.ID, Name: toolResult.Name, Response: object}})
		}
		event := session.Event{ID: raw.GetID().String(), Timestamp: raw.GetTimestamp(), Author: author, LLMResponse: model.LLMResponse{Content: content}}
		encoded, err := json.Marshal(event)
		if err != nil {
			return view.EntryView{}, err
		}
		value.Extensions = maps.Clone(value.Extensions)
		value.Extensions["adk:event"] = string(encoded)
		result.Raw[i] = value
	}
	return result, nil
}

func customEntry(raw entry.EntryLike) (entry.CustomEntry, bool) {
	switch value := raw.(type) {
	case entry.CustomEntry:
		return value, true
	case *entry.CustomEntry:
		return *value, true
	default:
		return entry.CustomEntry{}, false
	}
}
