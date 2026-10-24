package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/scbizu/anra/pkg/discovery"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRegistryCommand(settings *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "registry", Short: "Run the team's Agent Card directory", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			handler, err := discovery.NewHandler(settings.GetDuration("lease-ttl"))
			if err != nil {
				return err
			}
			listener, err := net.Listen("tcp", settings.GetString("listen"))
			if err != nil {
				return err
			}
			return serveHTTP(command.Context(), listener, handler, "registry", command.ErrOrStderr())
		},
	}
	command.Flags().String("listen", "127.0.0.1:9090", "directory HTTP listen address")
	command.Flags().Duration("lease-ttl", 15*time.Second, "registration lifetime without renewal (at least 1s)")
	return command
}

func serveHTTP(ctx context.Context, listener net.Listener, handler http.Handler, name string, status io.Writer) error {
	defer listener.Close()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer server.Close()
	if _, err := fmt.Fprintf(status, "%s listening at http://%s\n", name, listener.Addr()); err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
