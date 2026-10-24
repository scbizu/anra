package cmd

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/scbizu/anra/pkg/a2a"
	"github.com/scbizu/anra/pkg/agent"
	"github.com/scbizu/anra/pkg/discovery"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newServeCommand(settings *viper.Viper) *cobra.Command {
	command := &cobra.Command{
		Use: "serve", Short: "Expose a tape-backed agent over A2A HTTP", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) (runErr error) {
			a, tape, err := openAgent(command.Context(), settings)
			if err != nil {
				return err
			}
			defer func() { runErr = errors.Join(runErr, tape.Close()) }()
			return serve(command.Context(), a, settings, command.ErrOrStderr())
		},
	}
	modelFlags(command)
	command.Flags().String("listen", "127.0.0.1:8080", "HTTP listen address")
	return command
}

func serve(ctx context.Context, a *agent.Anra, settings *viper.Viper, status io.Writer) error {
	id := settings.GetString("agent")
	listener, err := net.Listen("tcp", settings.GetString("listen"))
	if err != nil {
		return err
	}
	defer listener.Close()
	base := "http://" + listener.Addr().String()
	card := &sdk.AgentCard{
		Name: id, Description: "Anra tape-backed agent", Version: "0.1.0",
		SupportedInterfaces: []*sdk.AgentInterface{sdk.NewAgentInterface(base+a2a.CorePath, sdk.TransportProtocolJSONRPC)},
		DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
		Skills: []sdk.AgentSkill{{ID: "chat", Name: "Chat", Description: "Talk to this agent", Tags: []string{"chat"}}},
	}
	executor, err := a2a.NewExecutor(a)
	if err != nil {
		return err
	}
	handler, err := a2a.NewMessageHandler(card, a2asrv.NewHandler(executor), a2a.TrustedPeerIdentity)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle(outboundPath, outboundHandler(a, id, settings))
	mux.Handle("/", handler)
	registered, cleanup, err := registerAgent(ctx, settings.GetString("registry"), discovery.Record{Name: id, CardURL: base + a2asrv.WellKnownAgentCardPath, Card: card})
	if err != nil {
		return err
	}
	defer cleanup()
	err = serveHTTP(registered, listener, mux, id, status)
	if err == nil && ctx.Err() == nil && context.Cause(registered) != nil {
		return context.Cause(registered)
	}
	return err
}
