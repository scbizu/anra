package e2e_test

import (
	"fmt"
	"path/filepath"

	"github.com/cucumber/godog"
)

func initializeEnvironment(sc *godog.ScenarioContext, c *cliScenario, team *teamScenario) {
	sc.Step(`^CLI environment settings for agent "([^"]*)"$`, c.agentEnvironment)
	sc.Step(`^I run the CLI using environment settings with "([^"]*)"$`, func(text string) error {
		c.execute("run", text)
		return c.err
	})
	sc.Step(`^the environment selects configured agent "([^"]*)" in session "([^"]*)"$`, func(id, session string) {
		c.env = map[string]string{"ANRA_CONFIG": c.configPath(), "ANRA_AGENT": id, "ANRA_SESSION": session}
	})
	sc.Step(`^I override the environment with agent "([^"]*)" and session "([^"]*)"$`, func(id, session string) error {
		c.execute("run", "--agent", id, "--session", session, "hello "+id)
		return c.err
	})
	sc.Step(`^the environment has an invalid provider and timeout$`, func() {
		c.env["ANRA_PROVIDER"] = "invalid"
		c.env["ANRA_TIMEOUT"] = "soon"
	})
	sc.Step(`^I run the CLI with explicit provider and timeout flags$`, func() error {
		c.execute("run", "--provider", "deepseek", "--timeout", "1m", "hello")
		return c.err
	})
	sc.Step(`^team agents "([^"]*)" and "([^"]*)" are serving using environment settings$`, func(a, b string) error {
		for _, id := range []string{a, b} {
			p := &cliScenario{s: c.s}
			p.agentEnvironment(id)
			p.env["ANRA_REGISTRY"] = team.registry.peer
			p.env["ANRA_LISTEN"] = "127.0.0.1:0"
			team.agents[id] = p
			if err := p.startProcess(id, "serve"); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^agent "([^"]*)" sends "([^"]*)" to "([^"]*)" using environment settings$`, func(id, text, to string) error {
		c.agentEnvironment(id)
		c.env["ANRA_REGISTRY"] = team.registry.peer
		c.env["ANRA_TO"] = to
		c.env["ANRA_PEER"] = "http://127.0.0.1:1"
		c.execute("send", text)
		return c.err
	})
	sc.Step(`^I run the CLI with environment problem "([^"]*)"$`, func(problem string) error {
		c.agentEnvironment("alice")
		args := []string{"run", "hello"}
		switch problem {
		case "invalid agent":
			c.env["ANRA_AGENT"] = "../outside"
		case "unknown provider":
			c.env["ANRA_PROVIDER"] = "invalid"
		case "invalid duration":
			c.env["ANRA_TIMEOUT"] = "soon"
		case "zero timeout":
			c.env["ANRA_TIMEOUT"] = "0s"
		case "empty session":
			c.env["ANRA_SESSION"] = ""
		case "missing config":
			c.env["ANRA_CONFIG"] = filepath.Join(c.s.root, "missing.toml")
		case "legacy peer destination":
			c.env["ANRA_PEER"] = "http://127.0.0.1:1"
			args = []string{"send", "hello"}
		case "missing model key":
			c.env["DEEPSEEK_API_KEY"] = ""
			c.env["ANRA_PROVIDER"] = "deepseek"
		default:
			return fmt.Errorf("unknown environment problem %q", problem)
		}
		c.execute(args...)
		return nil
	})
}

func (c *cliScenario) agentEnvironment(id string) {
	c.env = map[string]string{
		"ANRA_AGENT": id, "ANRA_DATA_DIR": c.dataDir(), "ANRA_SESSION": "default",
		"ANRA_PROVIDER": "deepseek", "ANRA_TIMEOUT": "2s",
	}
}
