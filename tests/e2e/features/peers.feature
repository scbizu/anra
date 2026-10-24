Feature: Trusted peers communicate and borrow permissioned context
  A borrower discovers a peer, speaks standard A2A, and reads only the context
  that the lender approved. Decisions and closures survive storage restarts.

  Background:
    Given a researcher peer with two findings and a durable grant journal

  Scenario: Send a message to a discovered peer using standard A2A
    When the borrower discovers the researcher and sends "hello"
    Then the peer answers "research response"
    And the peer can stream a response to "hello again"

  Scenario: Borrowed context informs an answer and survives a borrower restart
    When the borrower asks its agent to use the researcher's findings
    Then the answer is "Use version checks for competing writes."
    And the borrowed tool result retains the source and sequence provenance
    When I close and reopen the borrower agent's storage
    Then its model can recall the findings as a tool result
    And every stored borrower entry belongs to the borrower

  Scenario: A grant keeps its original snapshot across a lender restart
    When the borrower requests context
    Then the decision is "granted"
    When the borrower reads the first finding
    And the researcher adds a finding after the grant
    And the researcher closes and reopens its storage and server
    Then retrying the request returns the same grant
    And the borrower can finish paging the original snapshot

  Scenario: A selected range is fully read across multiple pages
    Given the researcher has 103 findings
    When the borrower requests context
    Then the borrower receives all 103 findings across multiple pages

  Scenario: Reusing a request ID with different input is rejected
    When the borrower requests context
    And the borrower changes the query but keeps the request ID
    Then the request fails with a conflict

  Scenario: Another identified peer cannot use the borrower's grant
    When the borrower requests context
    Then another peer cannot look up, read, or release that grant
    And another peer's own request is denied without context

  Scenario Outline: Ending a grant prevents further reads after restart
    When the borrower requests context
    And the grant is <closure>
    And the researcher closes and reopens its storage and server
    Then the borrower cannot read the grant
    And the recorded decision still identifies the original grant

    Examples:
      | closure  |
      | released |
      | revoked  |
      | expired  |

  Scenario Outline: A pending decision can be resolved and recovered
    Given the researcher requires approval for borrowing
    When the borrower requests context
    Then the decision is "pending"
    When the researcher closes and reopens its storage and server
    Then the decision is still "pending"
    When the researcher resolves the request as "<outcome>"
    And the researcher closes and reopens its storage and server
    Then the decision is still "<outcome>"
    And access matches the "<outcome>" decision

    Examples:
      | outcome |
      | granted |
      | denied  |

  Scenario Outline: Invalid or unauthorized wire requests cannot borrow context
    When a peer submits a wire request with "<problem>"
    Then the wire error is "<code>"

    Examples:
      | problem           | code                |
      | no identity       | unauthenticated     |
      | no extension      | failed_precondition |
      | missing purpose   | invalid_argument    |
      | reversed bounds   | invalid_argument    |
      | excessive page    | invalid_argument    |
      | excessive body    | resource_exhausted  |
