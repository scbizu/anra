Feature: Running agents register cards and communicate by name
  Alice and Bob both serve their own tape-backed runtimes. They discover one
  another through the team's directory and exchange messages directly.

  Background:
    Given a running team Agent Card directory

  Scenario: Both servers discover each other and exchange greetings in both directions
    Given team agents "alice" and "bob" are serving
    When I list the team's registered peers
    Then the team directory lists "alice" and "bob"
    When running agent "alice" sends "hello from alice" to "bob"
    Then the CLI answer is "bob: hello from alice"
    When running agent "bob" sends "hello from bob" to "alice"
    Then the CLI answer is "alice: hello from bob"
    When I stop the team agents
    Then the CLI agent "alice" has this conversation on its tape
      | role      | text                  |
      | user      | hello from alice      |
      | assistant | bob: hello from alice |
      | user      | hello from bob        |
      | assistant | alice: hello from bob |
    And the CLI agent "bob" has this conversation on its tape
      | role      | text                  |
      | user      | hello from alice      |
      | assistant | bob: hello from alice |
      | user      | hello from bob        |
      | assistant | alice: hello from bob |

  Scenario: Both servers can send while receiving a concurrent greeting
    Given team agents "alice" and "bob" are serving
    When both team agents send greetings simultaneously
    Then both team agents receive their peer's answer

  Scenario: A restarted server registers its new address and retains its tape
    Given team agents "alice" and "bob" are serving
    When running agent "alice" sends "first hello" to "bob"
    Then the CLI answer is "bob: first hello"
    When I stop team agent "bob"
    And I restart team agent "bob"
    And running agent "alice" sends "hello after restart" to "bob"
    Then the CLI answer is "bob: hello after restart"
    When I stop the team agents
    Then the CLI agent "bob" has this conversation on its tape
      | role      | text                     |
      | user      | first hello              |
      | assistant | bob: first hello         |
      | user      | hello after restart      |
      | assistant | bob: hello after restart |

  Scenario: A stopped agent unregisters and cannot receive named messages
    Given team agents "alice" and "bob" are serving
    When I stop team agent "bob"
    And running agent "alice" attempts to greet "bob"
    Then the CLI command fails with "agent is not registered"
    When I stop the team agents
    Then the CLI agent "alice" has an empty tape

  Scenario: Running agents register again after the directory restarts
    Given team agents "alice" and "bob" are serving
    When I restart the team directory at the same address
    Then both team agents eventually register again
    When running agent "alice" sends "hello after directory restart" to "bob"
    Then the CLI answer is "bob: hello after directory restart"

  Scenario: A CLI sender must itself be a running registered server
    When running agent "alice" attempts to greet "bob"
    Then the CLI command fails with "must be serving"
    And no CLI tape directory is created

  Scenario: A running agent name cannot be taken by another server
    Given team agents "alice" and "bob" are serving
    When another server attempts to register as "alice"
    Then the CLI command fails with "agent name is already registered"
    When running agent "bob" sends "still alice" to "alice"
    Then the CLI answer is "alice: still alice"

  Scenario: An agent that crashes disappears when its registration lease expires
    Given team agents "alice" and "bob" are serving
    When team agent "bob" crashes
    Then its registration eventually expires
    And team agent "alice" is still registered
    When running agent "alice" attempts to greet "bob"
    Then the CLI command fails with "agent is not registered"

  Scenario: Trusted agents register and communicate without a shared credential
    Given team agents "alice" and "bob" are serving
    When I list the team's registered peers
    Then the team directory lists "alice" and "bob"
    And the team cards require no bearer credential
    When running agent "alice" sends "hello without a token" to "bob"
    Then the CLI answer is "bob: hello without a token"

  Scenario: A different lease cannot renew or remove an active registration
    Given team agents "alice" and "bob" are serving
    When another lease attempts to renew or remove agent "alice"
    And I list the team's registered peers
    Then the team directory lists "alice" and "bob"
