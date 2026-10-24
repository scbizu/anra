package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/scbizu/anra/pkg/discovery"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newAskCommand(settings *viper.Viper) *cobra.Command {
	command := &cobra.Command{Use: "ask [flags] message", Short: "Ask a running agent to use its model and built-in tools", Args: messageArgs,
		RunE: func(command *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), settings.GetDuration("timeout"))
			defer cancel()
			directory, err := discovery.NewClient(settings.GetString("registry"))
			if err != nil {
				return err
			}
			record, err := directory.Lookup(ctx, settings.GetString("agent"))
			if err != nil {
				return err
			}
			base, err := discovery.BaseURL(record)
			if err != nil {
				return err
			}
			client, err := newPeerClient(ctx, base, settings.GetString("agent"))
			if err != nil {
				return err
			}
			if client.Name != settings.GetString("agent") {
				return errors.New("discovered card name mismatch")
			}
			answer, err := client.Send(ctx, message(args))
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), answer)
			return err
		},
	}
	command.Flags().Duration("timeout", time.Minute, "command timeout")
	return command
}
