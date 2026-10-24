package a2a

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

type MessageClient struct {
	client *a2aclient.Client
	Name   string
}

func NewMessageClient(ctx context.Context, h *http.Client, base string) (*MessageClient, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("peer must be an HTTP(S) base URL")
	}
	card, err := Discover(ctx, h, strings.TrimRight(base, "/")+a2asrv.WellKnownAgentCardPath)
	if err != nil {
		return nil, fmt.Errorf("discover peer: %w", err)
	}
	for _, endpoint := range card.SupportedInterfaces {
		if endpoint == nil {
			return nil, errors.New("peer advertised an empty interface")
		}
		remote, err := url.Parse(endpoint.URL)
		if err != nil || remote.Scheme != u.Scheme || remote.Host != u.Host {
			return nil, errors.New("peer advertised an endpoint outside its origin")
		}
	}
	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithJSONRPCTransport(h))
	if err != nil {
		return nil, err
	}
	return &MessageClient{client: client, Name: card.Name}, nil
}

// Send uses the official A2A transport and requires a direct text response.
func (r *MessageClient) Send(ctx context.Context, input string) (string, error) {
	result, err := r.client.SendMessage(ctx, &sdk.SendMessageRequest{Message: sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart(input))})
	if err != nil {
		return "", err
	}
	message, ok := result.(*sdk.Message)
	if !ok {
		return "", errors.New("peer returned a task instead of a text answer")
	}
	var text []string
	for _, part := range message.Parts {
		if part == nil {
			return "", errors.New("peer returned an empty message part")
		}
		if _, ok := part.Content.(sdk.Text); !ok {
			return "", errors.New("peer returned a non-text answer")
		}
		text = append(text, part.Text())
	}
	if strings.TrimSpace(strings.Join(text, "\n")) == "" {
		return "", errors.New("peer returned an empty answer")
	}
	return strings.Join(text, "\n"), nil
}
