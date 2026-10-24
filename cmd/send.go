package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newSendCommand(settings *viper.Viper) *cobra.Command {
	command := &cobra.Command{
		Use:   "send [flags] message",
		Short: "Ask a running agent to send a message to a named peer",
		Args:  messageArgs,
		PreRunE: func(*cobra.Command, []string) error {
			if settings.GetString("to") == "" {
				return errors.New("required flag(s): --to")
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), settings.GetDuration("timeout"))
			defer cancel()
			answer, err := sendFromServer(ctx, settings.GetString("agent"), settings.GetString("to"), message(args), settings)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), answer)
			return err
		},
	}
	command.Flags().String("to", "", "registered destination agent name (source must be serving)")
	command.Flags().Duration("timeout", time.Minute, "command timeout")
	return command
}
