// Package cmd defines the Anra command line interface.
package cmd

import "github.com/spf13/cobra"

// NewRootCommand returns an independent command tree with no shared flag state.
func NewRootCommand() *cobra.Command {
	settings := newEnvironment()
	root := &cobra.Command{
		Use:   "anra",
		Short: "Run tape-backed agents and communicate with peers",
		Long: `Run tape-backed agents and communicate with peers over A2A HTTP.

run/serve use tape-go's ADK runtime, configured models, and built-in tools.
DeepSeek requires DEEPSEEK_API_KEY.
Registry and peer endpoints must be accessible only to trusted team members.
Options also accept ANRA_ environment variables, such as ANRA_AGENT and ANRA_PROVIDER.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			return applyConfig(command, settings)
		},
	}
	root.PersistentFlags().String("agent", "anra", "local agent identity")
	root.PersistentFlags().String("data-dir", ".anra", "directory for per-agent tape databases")
	root.PersistentFlags().String("session", "default", "persistent conversation session")
	root.PersistentFlags().String("config", "", "TOML file containing agent settings")
	root.PersistentFlags().String("registry", "", "team Agent Card directory URL")
	root.AddCommand(newRunCommand(settings), newAskCommand(settings), newServeCommand(settings), newSendCommand(settings), newRegistryCommand(settings), newPeersCommand(settings))
	return root
}
