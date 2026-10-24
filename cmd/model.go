package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	deepseek "github.com/cohesion-org/deepseek-go"
	"github.com/scbizu/tape-go/pkg/provider/ds"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/adk/model"
)

func modelFlags(command *cobra.Command) {
	command.Flags().String("provider", "deepseek", "model provider")
	command.Flags().String("model", "", "model name (empty uses the provider default)")
	command.Flags().String("system", "", "system instruction for the agent")
	command.Flags().String("api-base", "", "provider API base URL override")
}

func newModel(settings *viper.Viper) (model.LLM, error) {
	provider := settings.GetString("provider")
	key := settings.GetString("deepseek-api-key")
	if provider != "deepseek" {
		return nil, fmt.Errorf("unknown provider %q (use deepseek)", provider)
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("DEEPSEEK_API_KEY is required for provider deepseek")
	}
	var opts []deepseek.Option
	if apiBase := settings.GetString("api-base"); apiBase != "" {
		u, err := url.Parse(apiBase)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("--api-base must be an HTTP(S) provider URL")
		}
		opts = append(opts, deepseek.WithBaseURL(strings.TrimRight(apiBase, "/")+"/"))
	}
	return ds.NewModel(key, settings.GetString("model"), opts...)
}
