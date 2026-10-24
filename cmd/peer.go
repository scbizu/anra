package cmd

import (
	"context"

	"github.com/scbizu/anra/pkg/a2a"
)

func newPeerClient(ctx context.Context, base, identity string) (*a2a.MessageClient, error) {
	return a2a.NewMessageClient(ctx, a2a.TrustedHTTPClient(identity), base)
}
