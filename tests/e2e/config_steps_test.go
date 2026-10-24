package e2e_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
)

func initializeConfig(sc *godog.ScenarioContext, c *cliScenario, team *teamScenario) {
	sc.Step(`^a TOML CLI configuration with Alice and Bob$`, func() error { return c.writeAgentConfig("") })
	sc.Step(`^a TOML CLI configuration for the team directory$`, func() error { return c.writeAgentConfig(team.registry.peer) })
	sc.Step(`^Alice's configured session is "([^"]*)"$`, func(session string) error {
		data, err := os.ReadFile(c.configPath())
		if err != nil {
			return err
		}
		text := strings.Replace(string(data), "[agents.alice]", fmt.Sprintf("[agents.alice]\nsession = %q", session), 1)
		return os.WriteFile(c.configPath(), []byte(text), 0o600)
	})
	sc.Step(`^I run the configured default agent in session "([^"]*)" with "([^"]*)"$`, func(session, text string) error {
		c.execute("run", "--config", c.configPath(), "--session", session, text)
		return c.err
	})
	sc.Step(`^the configured CLI agent "([^"]*)" has this conversation in session "([^"]*)"$`, func(id, session string, table *godog.Table) error {
		return c.conversationAt(c.dataDir(), id, session, table)
	})
	sc.Step(`^I run the configured default agent with "([^"]*)"$`, func(text string) error {
		c.execute("--config", c.configPath(), "run", text)
		return c.err
	})
	sc.Step(`^I run the configured CLI agent "([^"]*)" with "([^"]*)"$`, func(id, text string) error {
		c.execute("run", "--config", c.configPath(), "--agent", id, text)
		return c.err
	})
	sc.Step(`^the configured CLI agent "([^"]*)" is serving$`, func(id string) error {
		p := &cliScenario{s: c.s}
		team.agents[id] = p
		return p.startProcess(id, "--config", c.configPath(), "serve", "--agent", id)
	})
	sc.Step(`^the configured default agent sends "([^"]*)" to "([^"]*)"$`, func(text, to string) error {
		c.execute("send", "--config", c.configPath(), "--to", to, text)
		return c.err
	})
	sc.Step(`^the configured default agent overrides its registry flag and sends "([^"]*)" to "([^"]*)"$`, func(text, to string) error {
		if err := c.writeAgentConfig("http://127.0.0.1:1"); err != nil {
			return err
		}
		c.execute("send", "--config", c.configPath(), "--registry", team.registry.peer, "--to", to, text)
		return c.err
	})
	sc.Step(`^I run the configured default agent with an explicit storage directory$`, func() {
		c.execute("run", "--config", c.configPath(), "--data-dir", filepath.Join(c.s.root, "override"), "hello")
	})
	sc.Step(`^the exchange is stored only in the explicit directory$`, func(table *godog.Table) error {
		_, err := os.Stat(c.dataDir())
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("configured directory should not exist: %v", err)
		}
		return c.conversationAt(filepath.Join(c.s.root, "override"), "alice", "default", table)
	})
	sc.Step(`^I run the CLI with TOML configuration problem "([^"]*)"$`, c.invalidConfig)
}

func (c *cliScenario) configPath() string { return filepath.Join(c.s.root, "agents.toml") }

func (c *cliScenario) writeAgentConfig(registry string) error {
	endpoint := ""
	if registry != "" {
		endpoint = fmt.Sprintf("registry = %q\n", registry)
	}
	text := fmt.Sprintf(`default-agent = "alice"

[defaults]
data-dir = "cli"
session = "default"
provider = "invalid-default"
timeout = "2s"
listen = "127.0.0.1:0"
%s
[agents.alice]
provider = "deepseek"

[agents.bob]
provider = "deepseek"
listen = "127.0.0.1:0"
`, endpoint)
	return os.WriteFile(c.configPath(), []byte(text), 0o600)
}

func (c *cliScenario) invalidConfig(problem string) error {
	if err := c.writeAgentConfig(""); err != nil {
		return err
	}
	data, err := os.ReadFile(c.configPath())
	if err != nil {
		return err
	}
	text := string(data)
	args := []string{"run", "--config", c.configPath()}
	switch problem {
	case "missing file":
		if err := os.Remove(c.configPath()); err != nil {
			return err
		}
	case "malformed TOML":
		text += "\n[broken\n"
	case "wrong setting type":
		text = strings.ReplaceAll(text, `provider = "deepseek"`, `provider = 123`)
	case "unknown setting":
		text += "\nruner = 'echo'\n"
	case "agent peer setting":
		text += "\npeer = 'http://127.0.0.1:1'\n"
	case "default peer setting":
		text = strings.Replace(text, "[defaults]", "[defaults]\npeer = 'http://127.0.0.1:1'", 1)
	case "no agents":
		text = "[defaults]\nprovider = 'deepseek'\n"
	case "unknown selected agent":
		args = append(args, "--agent", "charlie")
	case "unknown default agent":
		text = strings.Replace(text, `default-agent = "alice"`, `default-agent = "charlie"`, 1)
	case "invalid agent name":
		text += "\n[agents.'../outside']\nprovider = 'deepseek'\n"
	case "invalid duration":
		text = strings.Replace(text, `timeout = "2s"`, `timeout = "soon"`, 1)
	case "explicit provider":
		args = append(args, "--provider", "invalid")
	case "explicit zero timeout":
		args = append(args, "--timeout", "0s")
	case "explicit empty session":
		args = append(args, "--session", "")
	default:
		return fmt.Errorf("unknown TOML problem %q", problem)
	}
	if problem != "missing file" {
		if err := os.WriteFile(c.configPath(), []byte(text), 0o600); err != nil {
			return err
		}
	}
	c.execute(append(args, "hello")...)
	return nil
}
