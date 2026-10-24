package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	buildCLI(t)
	format := "pretty"
	if reportPath := os.Getenv("ANRA_E2E_REPORT"); reportPath != "" {
		if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
			t.Fatal(err)
		}
		format += ",cucumber:" + reportPath
	}
	suite := godog.TestSuite{
		Name:                "anra",
		ScenarioInitializer: initializeScenario,
		Options: &godog.Options{
			Format:   format,
			Paths:    []string{"features"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("BDD end-to-end scenarios failed")
	}
}
