package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"
	"github.com/scbizu/anra/pkg/a2a"
	"github.com/scbizu/anra/pkg/discovery"
)

type teamScenario struct {
	c          *cliScenario
	registry   *cliScenario
	agents     map[string]*cliScenario
	concurrent []*cliScenario
	crashed    string
}

func initializeTeam(sc *godog.ScenarioContext, c *cliScenario) *teamScenario {
	t := &teamScenario{c: c}
	initializeADK(sc, t)
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*t = teamScenario{c: c, agents: make(map[string]*cliScenario)}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		err := t.stopAgents()
		if t.registry != nil {
			err = errors.Join(err, t.registry.stop())
		}
		return ctx, err
	})
	sc.Step(`^a running team Agent Card directory$`, func() error {
		t.registry = &cliScenario{s: c.s}
		return t.registry.startProcess("registry", "registry", "--listen", "127.0.0.1:0", "--lease-ttl", "1s")
	})
	sc.Step(`^team agents "([^"]*)" and "([^"]*)" are serving$`, func(a, b string) error {
		return t.startAgents(a, b)
	})
	sc.Step(`^I list the team's registered peers$`, func() { c.execute("peers", "--registry", t.registry.peer) })
	sc.Step(`^the team directory lists "([^"]*)" and "([^"]*)"$`, func(a, b string) error {
		if c.err != nil {
			return c.err
		}
		for _, name := range []string{a, b} {
			if !strings.Contains(c.output, name+"\t") {
				return fmt.Errorf("directory does not list %s: %s", name, c.output)
			}
		}
		return nil
	})
	sc.Step(`^running agent "([^"]*)" sends "([^"]*)" to "([^"]*)"$`, func(from, text, to string) error {
		t.send(c, from, to, text)
		return c.err
	})
	sc.Step(`^running agent "([^"]*)" attempts to greet "([^"]*)"$`, func(from, to string) { t.send(c, from, to, "hello") })
	sc.Step(`^I stop the team agents$`, t.stopAgents)
	sc.Step(`^I restart the team directory at the same address$`, func() error {
		address := strings.TrimPrefix(t.registry.peer, "http://")
		if err := t.registry.stop(); err != nil {
			return err
		}
		return t.registry.startProcess("registry", "registry", "--listen", address, "--lease-ttl", "1s")
	})
	sc.Step(`^both team agents eventually register again$`, func() error {
		client, err := discovery.NewClient(t.registry.peer)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(c.s.ctx, 4*time.Second)
		defer cancel()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			records, err := client.List(ctx)
			if err != nil {
				return err
			}
			if len(records) == 2 {
				return nil
			}
			select {
			case <-ctx.Done():
				return errors.New("agents did not register after directory restart")
			case <-ticker.C:
			}
		}
	})
	sc.Step(`^I stop team agent "([^"]*)"$`, func(id string) error { return t.agents[id].stop() })
	sc.Step(`^I restart team agent "([^"]*)"$`, func(id string) error {
		return t.agents[id].startProcess(id, "serve", "--config", c.configPath(), "--agent", id)
	})
	sc.Step(`^team agent "([^"]*)" is still registered$`, func(id string) error {
		client, err := discovery.NewClient(t.registry.peer)
		if err != nil {
			return err
		}
		_, err = client.Lookup(c.s.ctx, id)
		return err
	})
	sc.Step(`^another server attempts to register as "([^"]*)"$`, func(id string) {
		c.execute("serve", "--agent", id, "--provider", "deepseek", "--data-dir", filepath.Join(c.s.root, "duplicate"), "--listen", "127.0.0.1:0", "--registry", t.registry.peer)
	})
	sc.Step(`^both team agents send greetings simultaneously$`, func() {
		t.concurrent = []*cliScenario{{s: c.s}, {s: c.s}}
		var group sync.WaitGroup
		for i, from := range []string{"alice", "bob"} {
			group.Add(1)
			go func(i int, from string) {
				defer group.Done()
				to := "alice"
				if from == "alice" {
					to = "bob"
				}
				t.send(t.concurrent[i], from, to, "hello from "+from)
			}(i, from)
		}
		group.Wait()
	})
	sc.Step(`^both team agents receive their peer's answer$`, func() error {
		for i, want := range []string{"bob: hello from alice", "alice: hello from bob"} {
			p := t.concurrent[i]
			if p.err != nil || strings.TrimSpace(p.output) != want {
				return fmt.Errorf("concurrent send = %q, error %v; want %q", p.output, p.err, want)
			}
		}
		return nil
	})
	sc.Step(`^team agent "([^"]*)" crashes$`, func(id string) error {
		p := t.agents[id]
		if err := p.server.Process.Kill(); err != nil {
			return err
		}
		<-p.done
		p.server = nil
		t.crashed = id
		return nil
	})
	sc.Step(`^its registration eventually expires$`, func() error {
		client, err := discovery.NewClient(t.registry.peer)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(c.s.ctx, 4*time.Second)
		defer cancel()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			_, err := client.Lookup(ctx, t.crashed)
			if err != nil && strings.Contains(err.Error(), "HTTP 404") {
				return nil
			}
			if err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return errors.New("crashed agent registration did not expire")
			case <-ticker.C:
			}
		}
	})
	sc.Step(`^another lease attempts to renew or remove agent "([^"]*)"$`, func(name string) error {
		client, err := discovery.NewClient(t.registry.peer)
		if err != nil {
			return err
		}
		record, err := client.Lookup(c.s.ctx, name)
		if err != nil {
			return err
		}
		if _, err := client.Register(c.s.ctx, record, "another-lease"); err == nil || !strings.Contains(err.Error(), "HTTP 409") {
			return fmt.Errorf("unexpected renewal result: %v", err)
		}
		if err := client.Unregister(c.s.ctx, name, "another-lease"); err == nil || !strings.Contains(err.Error(), "HTTP 409") {
			return fmt.Errorf("unexpected removal result: %v", err)
		}
		return nil
	})
	sc.Step(`^the team cards require no bearer credential$`, func() error {
		for name, p := range t.agents {
			card, err := a2a.Discover(c.s.ctx, http.DefaultClient, p.peer+"/.well-known/agent-card.json")
			if err != nil {
				return err
			}
			if len(card.SecuritySchemes) != 0 || len(card.SecurityRequirements) != 0 {
				return fmt.Errorf("agent %s advertises authentication requirements", name)
			}
		}
		return nil
	})
	return t
}

func (t *teamScenario) startAgents(a, b string) error {
	config := fmt.Sprintf("[defaults]\nregistry = %q\ndata-dir = 'cli'\nprovider = 'deepseek'\nlisten = '127.0.0.1:0'\n[agents.%s]\n[agents.%s]\n", t.registry.peer, a, b)
	if err := os.WriteFile(t.c.configPath(), []byte(config), 0o600); err != nil {
		return err
	}
	for _, id := range []string{a, b} {
		p := &cliScenario{s: t.c.s}
		t.agents[id] = p
		if err := p.startProcess(id, "serve", "--config", t.c.configPath(), "--agent", id); err != nil {
			return err
		}
	}
	return nil
}

func (t *teamScenario) send(c *cliScenario, from, to, text string) {
	c.execute("send", "--agent", from, "--registry", t.registry.peer, "--to", to, text)
}

func (t *teamScenario) stopAgents() error {
	var err error
	for _, p := range t.agents {
		err = errors.Join(err, p.stop())
	}
	return err
}
