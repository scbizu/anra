package cmd

import (
	"strings"

	"github.com/spf13/viper"
)

// Each command tree owns its Viper instance, including credential bindings.
func newEnvironment() *viper.Viper {
	settings := viper.New()
	settings.SetEnvPrefix("ANRA")
	settings.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	settings.AllowEmptyEnv(true)
	settings.MustBindEnv("deepseek-api-key", "DEEPSEEK_API_KEY")
	return settings
}
