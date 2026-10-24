package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/scbizu/anra/pkg/discovery"
)

func registerAgent(parent context.Context, endpoint string, record discovery.Record) (context.Context, func(), error) {
	if endpoint == "" {
		return parent, func() {}, nil
	}
	client, err := discovery.NewClient(endpoint)
	if err != nil {
		return nil, nil, err
	}
	lease := uuid.NewString()
	ttl, err := client.Register(parent, record, lease)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if nextTTL, err := client.Register(ctx, record, lease); err != nil {
					var rejection *discovery.HTTPError
					if errors.As(err, &rejection) && rejection.StatusCode >= 400 && rejection.StatusCode < 500 {
						cancel(err)
						return
					}
					// A temporary directory outage must not stop the agent's runtime.
				} else {
					ticker.Reset(nextTTL / 3)
				}
			}
		}
	}()
	cleanup := func() {
		cancel(context.Canceled)
		<-done
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = client.Unregister(ctx, record.Name, lease)
	}
	return ctx, cleanup, nil
}
