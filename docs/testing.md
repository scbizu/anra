# Testing Anra

The repository keeps BDD end-to-end scenarios as its shared acceptance contract.
Read the [agent features](../tests/e2e/features/agent.feature) and
[peer features](../tests/e2e/features/peers.feature), plus the
[CLI features](../tests/e2e/features/cli.feature) and
[TOML configuration features](../tests/e2e/features/config.feature), plus the
[environment features](../tests/e2e/features/env.feature), to review what Anra
promises.
The [running-agent discovery features](../tests/e2e/features/discovery.feature)
cover two registered servers, bidirectional and concurrent greetings, restart,
duplicate names, registration lease ownership, token-free operation, graceful
unregistering, and crash expiry.
The [ADK features](../tests/e2e/features/adk.feature) cover built-in tool execution,
model-driven peer discovery and messaging, persisted tool history, and rejection
of tool configuration. The same Given/When/Then steps execute with
[Godog](https://github.com/cucumber/godog/tree/v0.15.1), so the specifications
and checks stay together. Undefined and pending steps fail the suite.

## Running scenarios

```sh
mise run test  # Build, schema checks, generated bindings, and E2E scenarios.
mise run e2e   # Only the BDD E2E scenarios.
# Go alone, without mise:
go test -count=1 -v ./tests/e2e
# Select a scenario using its Go subtest name:
go test -count=1 -v ./tests/e2e -run 'TestFeatures/A_grant_keeps_its_original_snapshot'
```

## Investigating in the browser

Install the optional reporter with Node.js 22 or newer and npm:

```sh
npm ci
mise run e2e-report
# Equivalent command, with optional Go test arguments:
npm run e2e:report
npm run e2e:report -- -run 'TestFeatures/A_grant_keeps_its_original_snapshot'
# Open the latest report on macOS:
open reports/e2e/latest.html
```

The [Cucumber HTML Reporter](https://github.com/gkushang/cucumber-html-reporter)
shows features, scenarios, Given/When/Then results, durations, and failure
details. Reports are generated after failed test runs too, and the command
still exits with the test failure. A Go build failure cannot produce scenario
results; check the console output in that case.

Each run writes `reports/e2e/<timestamp>/index.html` and its `cucumber.json`.
`reports/e2e/latest.html` opens the newest report; older reports remain available.
On scenario failure, its closed database files and a `failure.json` snapshot
are saved under that run's `artifacts/scenario-*/` directory. The snapshot
includes the scenario name, error, model request, borrow request, decision, and
first page when available. These are separate files to inspect alongside the
HTML report; HTTP traffic is not captured automatically.

Dependencies and generated reports are ignored by Git. Delete `reports/e2e/`
when you no longer need its run history. Routine `mise run test` and `mise run e2e`
continue to work with Go alone; Node is used only for HTML reporting.

Scenarios use real Anra agents, tape-go bbolt storage, the grant journal,
discovery, the official A2A SDK, and Connect over localhost HTTP. Each scenario
gets its own temporary files and servers, with cleanup after success or failure.
Reporting runs preserve a copy of failed scenarios' files before cleanup.
Restart steps close and reopen the database files and rebuild the server on a
new port. Pagination checks include a view larger than one full page. Expiry
uses a controlled clock rather than sleeps.

Model responses and lender permission decisions are deterministic fixtures.
The suite needs no model credentials or external service once Go dependencies
are installed. It checks Anra's behavior around model calls; it does not measure
a real model's answer quality. Library scenarios use public Go APIs and peer
HTTP endpoints. CLI scenarios build `cmd/anra` once per suite and launch real
processes using tape-go's ADK runtime and real provider HTTP requests to a
deterministic local model fixture, temporary disk storage, and localhost HTTP. They check local persistence, two-agent messaging, receiver restart,
missing or malformed caller identities, TOML agent selection, Viper environment settings,
configuration precedence, running-agent discovery, and invalid configuration.
All CLI sends use registered agent names. Direct peer flags and TOML settings
are rejected; the legacy peer environment variable cannot select a destination.
Directory lease expiry uses bounded polling of the public directory API;
the crashing process is killed while the remaining agent continues renewing.
Each subprocess has
inherited `ANRA_` settings and model credentials removed from its environment;
scenario fixtures supply only deterministic settings and model fixture keys.
TapeView scenarios also use trusted caller identities without shared tokens,
including denial when another identified peer tries to use a borrower's grant.
The suite does not exercise an
application UI.

## Temporary unit checks

Agents should write unit tests when useful for diagnosis or implementation.
These are local evaluation tools and must not be committed. The repository
ignores `*_test.go` outside `tests/e2e/`; ignored Go files still run when their
package is tested:

```sh
go test ./pkg/a2a -run TestLocalInvestigation
```

Remove disposable tests after use. Before proposing a change, check that its
diff contains only the intended product changes and shared E2E scenarios.
The `AGENTS.md` policy carries this workflow to future agent sessions.

## Adding behavior

Start with a concrete scenario describing a person's goal and observable
outcome. Reuse steps where their meaning is the same, and exercise the public
boundary through the actual runtime, transport, and storage. Keep setup and
implementation detail in the step definitions, not in the feature language.
Each scenario must stand alone. Include denied access and failure behavior
when they are part of the promise being changed.

This policy favors a human-readable acceptance contract. Temporary unit checks
can make debugging faster, but their assertions are not retained as a separate
regression suite. BDD scenarios should therefore preserve important observable
failure cases as the product evolves.
