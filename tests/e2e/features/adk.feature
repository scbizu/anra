Feature: ADK runs each agent with its configured model and built-in tools
  Production agents use tape-go's ADK runtime. Deterministic model fixtures
  exercise real model HTTP requests, tool execution, peer HTTP, and tape storage.

  Scenario: An agent discovers and messages a peer through its model's tools
    Given a running team Agent Card directory
    And team agents "alice" and "bob" are serving
    When I ask running agent "alice" to "discover peers and ask bob to say hello"
    Then the CLI answer is "bob: hello from alice"
    When I stop team agent "alice"
    Then agent "alice" has persisted these ADK tool calls and results
      | tool              |
      | discover_peers    |
      | send_peer_message |
    When I restart team agent "alice"
    And I ask running agent "alice" to "recall the peer answer"
    Then the CLI answer is "bob: hello from alice"
    When I stop the team agents
    Then the CLI agent "bob" has this conversation on its tape
      | role      | text                  |
      | user      | hello from alice      |
      | assistant | bob: hello from alice |

  Scenario: An agent can use tape-go's native Bash tool through ADK
    Given the environment attempts to select an injected tool
    When I run the local CLI agent with its built-in Bash tool
    Then the CLI answer is "anra-bash-test"
    And agent "alice" has persisted these ADK tool calls and results
      | tool |
      | bash |

  Scenario Outline: Production configuration separates providers from test fixtures
    When I run the CLI with unsupported ADK configuration "<problem>"
    Then the CLI command fails with "<error>"
    And no CLI tape directory is created

    Examples:
      | problem            | error             |
      | runner flag        | unknown flag      |
      | echo provider      | unknown provider  |
      | tool flag          | unknown flag      |
      | tool config        | parse config      |
      | invalid API base   | --api-base        |

  Scenario: Existing model and tool history remains readable after adopting ADK
    Given an agent with persisted legacy model and tool history
    When I ask "follow-up" and the model answers "history retained"
    Then the answer is "history retained"
    And the model retains legacy tool history without rewriting old entries
