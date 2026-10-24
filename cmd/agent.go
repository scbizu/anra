package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/scbizu/anra/pkg/a2a"
	"github.com/scbizu/anra/pkg/agent"
	"github.com/scbizu/anra/pkg/discovery"
	"github.com/scbizu/tape-go/pkg/tape/owner"
	"github.com/scbizu/tape-go/pkg/tape/storage/bbolt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func message(args []string) string { return strings.Join(args, " ") }

func messageArgs(_ *cobra.Command, args []string) error {
	if strings.TrimSpace(message(args)) == "" {
		return errors.New("a non-empty message is required")
	}
	return nil
}

// openAgent reads the resolved settings and opens an agent and its tape.
// The caller owns execution and must close the returned tape.
func openAgent(ctx context.Context, settings *viper.Viper) (*agent.Anra, *bbolt.Bbolt, error) {
	llm, err := newModel(settings)
	if err != nil {
		return nil, nil, err
	}
	id := settings.GetString("agent")
	options := []agent.Option{
		agent.WithLLM(llm),
		agent.WithSystemInstruction("You are Anra agent " + id + ".\n" + settings.GetString("system")),
	}
	if registry := settings.GetString("registry"); registry != "" {
		directory, err := discovery.NewClient(registry)
		if err != nil {
			return nil, nil, err
		}
		options = append(options, agent.WithPeers(directory, a2a.TrustedHTTPClient(id)))
	}
	tape, err := bbolt.NewBboltStorage(settings.GetString("session"), filepath.Join(settings.GetString("data-dir"), id+".db"))
	if err != nil {
		return nil, nil, err
	}
	if err := tape.Init(owner.WithOwnerId(ctx, id)); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("open agent tape: %w", err), tape.Close())
	}
	a, err := agent.New(id, tape, options...)
	if err != nil {
		return nil, nil, errors.Join(err, tape.Close())
	}
	return a, tape, nil
}
