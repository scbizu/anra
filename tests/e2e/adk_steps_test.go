package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cucumber/godog"
	"github.com/scbizu/tape-go/pkg/runner"
	"github.com/scbizu/tape-go/pkg/tape/entry"
	"github.com/scbizu/tape-go/pkg/tape/owner"
	"github.com/scbizu/tape-go/pkg/tape/storage/bbolt"
	"github.com/scbizu/tape-go/pkg/tape/view"
	"google.golang.org/adk/session"
)

func initializeADK(sc *godog.ScenarioContext, t *teamScenario) {
	c := t.c
	sc.Step(`^the environment attempts to select an injected tool$`, func() { c.env = map[string]string{"ANRA_TOOLS": "injected"} })
	sc.Step(`^an agent with persisted legacy model and tool history$`, func() error {
		s := c.s
		if err := s.openLocalTape(); err != nil {
			return err
		}
		messages := []runner.Message{
			{Role: runner.RoleUser, Text: "old question"},
			{Role: runner.RoleAssistant, ToolCalls: []runner.ToolCall{{ID: "old-call", Name: "borrow_peer_context", Arguments: json.RawMessage(`{"purpose":"research"}`)}}},
			{Role: runner.RoleTool, ToolResults: []runner.ToolResult{{ID: "old-call", Name: "borrow_peer_context", Result: json.RawMessage(`{"answer":"old finding"}`)}}},
			{Role: runner.RoleAssistant, Text: "old answer"},
		}
		kinds := []entry.EntryKind{entry.EntryUser, entry.EntryToolCall, entry.EntryToolResult, entry.EntryAssistant}
		for i, message := range messages {
			encoded, err := json.Marshal(message)
			if err != nil {
				return err
			}
			raw := entry.CustomEntry{Entry: entry.NewEntry(entry.WithEntryKind(kinds[i]), entry.WithEntryContent(message.Text), entry.WithEntryOwner("borrower-local")), Extensions: map[string]any{"anra:runner-message/v1": string(encoded)}}
			if _, err := s.localTape.Store(owner.WithOwnerId(s.ctx, "borrower-local"), raw); err != nil {
				return err
			}
		}
		return s.reopenLocalTape()
	})
	sc.Step(`^the model retains legacy tool history without rewriting old entries$`, func() error {
		s := c.s
		messages := s.lastRequest.Messages
		if len(messages) != 5 || messages[0].Text != "old question" || messages[3].Text != "old answer" || messages[4].Text != "follow-up" {
			return fmt.Errorf("unexpected legacy conversation: %+v", messages)
		}
		if len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].ID != "old-call" || string(messages[1].ToolCalls[0].Arguments) != `{"purpose":"research"}` {
			return fmt.Errorf("lost legacy call: %+v", messages[1])
		}
		if len(messages[2].ToolResults) != 1 || messages[2].ToolResults[0].ID != "old-call" || string(messages[2].ToolResults[0].Result) != `{"answer":"old finding"}` {
			return fmt.Errorf("lost legacy result: %+v", messages[2])
		}
		stored, err := s.localTape.Range(owner.WithOwnerId(s.ctx, "borrower-local"), view.EntryRange{SeqS: entry.SeqFromUint64(1), SeqE: entry.SeqFromUint64(5)})
		if err != nil {
			return err
		}
		if len(stored.Raw) != 4 {
			return fmt.Errorf("legacy entries: %d", len(stored.Raw))
		}
		for _, raw := range stored.Raw {
			var extensions map[string]any
			switch v := raw.(type) {
			case entry.CustomEntry:
				extensions = v.Extensions
			case *entry.CustomEntry:
				extensions = v.Extensions
			default:
				return fmt.Errorf("legacy entry changed type: %T", raw)
			}
			if _, ok := extensions["anra:runner-message/v1"]; !ok {
				return fmt.Errorf("legacy payload lost")
			}
			if _, ok := extensions["adk:event"]; ok {
				return fmt.Errorf("legacy entry was rewritten")
			}
		}
		return nil
	})
	sc.Step(`^I ask running agent "([^"]*)" to "([^"]*)"$`, func(id, text string) error {
		c.execute("ask", "--agent", id, "--registry", t.registry.peer, text)
		if c.err != nil {
			return fmt.Errorf("%w: %s", c.err, c.output)
		}
		return nil
	})
	sc.Step(`^I run the local CLI agent with its built-in Bash tool$`, func() error {
		c.execute("run", "--agent", "alice", "--data-dir", c.dataDir(), "run the built-in bash tool")
		if c.err != nil {
			return fmt.Errorf("%w: %s", c.err, c.output)
		}
		return nil
	})
	sc.Step(`^agent "([^"]*)" has persisted these ADK tool calls and results$`, func(id string, table *godog.Table) error {
		tape, err := bbolt.NewBboltStorage("default", filepath.Join(c.dataDir(), id+".db"))
		if err != nil {
			return err
		}
		defer tape.Close()
		ctx := owner.WithOwnerId(c.s.ctx, id)
		if err := tape.Init(ctx); err != nil {
			return err
		}
		state, err := tape.Get(ctx)
		if err != nil {
			return err
		}
		values, err := tape.Range(ctx, view.EntryRange{SeqS: entry.SeqFromUint64(1), SeqE: state.Scope.SeqE.Next()})
		if err != nil {
			return err
		}
		calls, results := map[string]bool{}, map[string]bool{}
		for _, raw := range values.Raw {
			var extensions map[string]any
			switch value := raw.(type) {
			case entry.CustomEntry:
				extensions = value.Extensions
			case *entry.CustomEntry:
				extensions = value.Extensions
			}
			payload, ok := extensions["adk:event"].(string)
			if !ok {
				return fmt.Errorf("entry %s is missing its ADK event", raw.GetID())
			}
			var event session.Event
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				return err
			}
			if event.Content == nil {
				continue
			}
			for _, part := range event.Content.Parts {
				if part.FunctionCall != nil {
					calls[part.FunctionCall.Name] = true
					if raw.GetKind() != entry.EntryToolCall {
						return fmt.Errorf("tool call has kind %v", raw.GetKind())
					}
				}
				if part.FunctionResponse != nil {
					results[part.FunctionResponse.Name] = true
					if raw.GetKind() != entry.EntryToolResult {
						return fmt.Errorf("tool result has kind %v", raw.GetKind())
					}
				}
			}
		}
		for _, row := range table.Rows[1:] {
			name := row.Cells[0].Value
			if !calls[name] || !results[name] {
				return fmt.Errorf("tool %s lacks a stored call/result: calls=%v results=%v", name, calls, results)
			}
		}
		return nil
	})
	sc.Step(`^I run the CLI with unsupported ADK configuration "([^"]*)"$`, func(problem string) error {
		args := []string{"run", "--agent", "alice", "--data-dir", c.dataDir()}
		switch problem {
		case "runner flag":
			args = append(args, "--runner", "echo")
		case "echo provider":
			args = append(args, "--provider", "echo")
		case "tool flag":
			args = append(args, "--tools", "unknown")
		case "tool config":
			if err := os.WriteFile(c.configPath(), []byte("[agents.alice]\ntools = [\"bash\"]\n"), 0600); err != nil {
				return err
			}
			args = append(args, "--config", c.configPath())
		case "invalid API base":
			args = append(args, "--api-base", "invalid")
		default:
			return fmt.Errorf("unknown ADK problem %q", problem)
		}
		c.execute(append(args, "hello")...)
		return nil
	})
}
