package a2a

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/scbizu/anra/pkg/discovery"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type discoverArgs struct{}
type discoverResult struct {
	Agents []map[string]any `json:"agents"`
}
type sendArgs struct {
	Peer    string `json:"peer" jsonschema:"Name of the registered destination agent."`
	Message string `json:"message" jsonschema:"Message or task for the destination agent."`
}
type sendResult struct {
	Peer   string `json:"peer"`
	Answer string `json:"answer"`
}

// PeerTools creates native ADK tools backed by the team's leased directory and
// official A2A SDK. Tool calls and responses are persisted by the ADK runtime.
func PeerTools(directory *discovery.Client, h *http.Client, self string) (map[string]tool.Tool, error) {
	discover, err := functiontool.New(functiontool.Config{Name: "discover_peers", Description: "Discover available peer agents and inspect their Agent Cards, skills, and endpoints."}, func(ctx tool.Context, _ discoverArgs) (discoverResult, error) {
		if directory == nil {
			return discoverResult{}, errors.New("peer discovery requires a configured registry")
		}
		records, err := directory.List(ctx)
		if err != nil {
			return discoverResult{}, err
		}
		result := discoverResult{Agents: make([]map[string]any, 0, len(records))}
		for _, record := range records {
			if record.Name != self {
				// SDK cards contain custom JSON union types. Expose their wire
				// representation so ADK validates the actual JSON shape.
				encoded, err := json.Marshal(record)
				if err != nil {
					return discoverResult{}, err
				}
				var value map[string]any
				if err := json.Unmarshal(encoded, &value); err != nil {
					return discoverResult{}, err
				}
				result.Agents = append(result.Agents, value)
			}
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	send, err := functiontool.New(functiontool.Config{Name: "send_peer_message", Description: "Send a message or delegate a task to a discovered peer, and receive its answer."}, func(ctx tool.Context, args sendArgs) (sendResult, error) {
		if directory == nil || h == nil {
			return sendResult{}, errors.New("peer messaging requires a configured registry")
		}
		if args.Peer == self || strings.TrimSpace(args.Message) == "" {
			return sendResult{}, errors.New("a different peer and a non-empty message are required")
		}
		record, err := directory.Lookup(ctx, args.Peer)
		if err != nil {
			return sendResult{}, err
		}
		base, err := discovery.BaseURL(record)
		if err != nil {
			return sendResult{}, err
		}
		client, err := NewMessageClient(ctx, h, base)
		if err != nil {
			return sendResult{}, err
		}
		if client.Name != args.Peer {
			return sendResult{}, errors.New("discovered card name mismatch")
		}
		answer, err := client.Send(ctx, args.Message)
		return sendResult{Peer: args.Peer, Answer: answer}, err
	})
	if err != nil {
		return nil, err
	}
	return map[string]tool.Tool{"discover_peers": discover, "send_peer_message": send}, nil
}
