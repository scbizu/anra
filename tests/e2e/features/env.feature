Feature: Configure CLI agents through the environment
  People can supply agent settings and credentials through environment
  variables, override TOML settings, and still take precedence with flags.

  Scenario: Run an agent entirely from environment settings
    Given CLI environment settings for agent "alice"
    When I run the CLI using environment settings with "hello"
    Then the CLI answer is "alice: hello"
    And the CLI agent "alice" has this conversation on its tape
      | role      | text         |
      | user      | hello        |
      | assistant | alice: hello |

  Scenario: Environment selects a configured agent and flags override the environment
    Given a TOML CLI configuration with Alice and Bob
    And the environment selects configured agent "bob" in session "environment"
    When I run the CLI using environment settings with "hello bob"
    Then the CLI answer is "bob: hello bob"
    When I override the environment with agent "alice" and session "default"
    Then the CLI answer is "alice: hello alice"
    And the configured CLI agent "bob" has this conversation in session "environment"
      | role      | text           |
      | user      | hello bob      |
      | assistant | bob: hello bob |
    And the configured CLI agent "alice" has this conversation in session "default"
      | role      | text               |
      | user      | hello alice        |
      | assistant | alice: hello alice |

  Scenario: Explicit flags override invalid environment values
    Given CLI environment settings for agent "alice"
    And the environment has an invalid provider and timeout
    When I run the CLI with explicit provider and timeout flags
    Then the CLI answer is "alice: hello"

  Scenario: Two agents communicate using environment settings
    Given a running team Agent Card directory
    And team agents "alice" and "bob" are serving using environment settings
    When agent "alice" sends "hello from alice" to "bob" using environment settings
    Then the CLI answer is "bob: hello from alice"
    When I stop the team agents
    Then the CLI agent "alice" has this conversation on its tape
      | role      | text                  |
      | user      | hello from alice      |
      | assistant | bob: hello from alice |
    And the CLI agent "bob" has this conversation on its tape
      | role      | text                  |
      | user      | hello from alice      |
      | assistant | bob: hello from alice |

  Scenario Outline: Invalid environment settings fail before opening storage
    When I run the CLI with environment problem "<problem>"
    Then the CLI command fails with "<error>"
    And no CLI tape directory is created

    Examples:
      | problem                 | error                       |
      | invalid agent           | --agent must start          |
      | unknown provider        | unknown provider            |
      | invalid duration        | timeout                     |
      | zero timeout            | --timeout must be positive  |
      | empty session           | --session must not be empty |
      | missing config          | read config                 |
      | legacy peer destination | required flag(s): --to      |
      | missing model key       | DEEPSEEK_API_KEY            |
