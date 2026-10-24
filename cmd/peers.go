package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/scbizu/anra/pkg/discovery"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newPeersCommand(settings *viper.Viper) *cobra.Command {
	return &cobra.Command{Use: "peers", Short: "List registered agents and their cards", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			client, err := discovery.NewClient(settings.GetString("registry"))
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(command.Context(), 5*time.Second)
			defer cancel()
			cards, err := client.List(ctx)
			if err != nil {
				return err
			}
			for _, record := range cards {
				if _, err := fmt.Fprintf(command.OutOrStdout(), "%s\t%s\n", record.Name, record.CardURL); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
