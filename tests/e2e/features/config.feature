Feature: Manage named CLI agents in a TOML file
  People can keep their agents' settings in one file and override them with
  command flags while keeping each agent's tape separate.

  Scenario: Select a default agent or another named agent from TOML
    Given a TOML CLI configuration with Alice and Bob
    When I run the configured default agent with "hello alice"
    Then the CLI answer is "alice: hello alice"
    When I run the configured CLI agent "bob" with "hello bob"
    Then the CLI answer is "bob: hello bob"
    And the CLI agent "alice" has this conversation on its tape
      | role      | text               |
      | user      | hello alice        |
      | assistant | alice: hello alice |
    And the CLI agent "bob" has this conversation on its tape
      | role      | text           |
      | user      | hello bob      |
      | assistant | bob: hello bob |

  Scenario: Serve and send using configured agents and override the registry
    Given a running team Agent Card directory
    And a TOML CLI configuration for the team directory
    And the configured CLI agent "alice" is serving
    And the configured CLI agent "bob" is serving
    When the configured default agent sends "hello from alice" to "bob"
    Then the CLI answer is "bob: hello from alice"
    When the configured default agent overrides its registry flag and sends "hello again" to "bob"
    Then the CLI answer is "bob: hello again"
    When I stop the team agents
    Then the CLI agent "alice" has this conversation on its tape
      | role      | text                  |
      | user      | hello from alice      |
      | assistant | bob: hello from alice |
      | user      | hello again           |
      | assistant | bob: hello again      |
    And the CLI agent "bob" has this conversation on its tape
      | role      | text                  |
      | user      | hello from alice      |
      | assistant | bob: hello from alice |
      | user      | hello again           |
      | assistant | bob: hello again      |

  Scenario: An explicit storage flag overrides a configured relative directory
    Given a TOML CLI configuration with Alice and Bob
    When I run the configured default agent with an explicit storage directory
    Then the CLI answer is "alice: hello"
    And the exchange is stored only in the explicit directory
      | role      | text         |
      | user      | hello        |
      | assistant | alice: hello |

  Scenario: Override a configured session with the built-in default value
    Given a TOML CLI configuration with Alice and Bob
    And Alice's configured session is "research"
    When I run the configured default agent with "research question"
    Then the CLI answer is "alice: research question"
    When I run the configured default agent in session "default" with "fresh question"
    Then the CLI answer is "alice: fresh question"
    And the configured CLI agent "alice" has this conversation in session "research"
      | role      | text                     |
      | user      | research question        |
      | assistant | alice: research question |
    And the configured CLI agent "alice" has this conversation in session "default"
      | role      | text                  |
      | user      | fresh question        |
      | assistant | alice: fresh question |

  Scenario Outline: Reject invalid config files and respect explicit flag values
    When I run the CLI with TOML configuration problem "<problem>"
    Then the CLI command fails with "<error>"
    And no CLI tape directory is created

    Examples:
      | problem                | error                       |
      | missing file           | read config                 |
      | malformed TOML         | parse config                |
      | wrong setting type     | parse config                |
      | unknown setting        | parse config                |
      | agent peer setting     | parse config                |
      | default peer setting   | parse config                |
      | no agents              | at least one agent          |
      | unknown selected agent | is not configured           |
      | unknown default agent  | is not configured           |
      | invalid agent name     | invalid agent name          |
      | invalid duration       | timeout                     |
      | explicit provider      | unknown provider            |
      | explicit zero timeout  | --timeout must be positive  |
      | explicit empty session | --session must not be empty |
