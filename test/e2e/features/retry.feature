Feature: --retry
  Requests that fail with a server error are retried. --retry is how many
  times; -1 means as many as it takes.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """

  Scenario: 2 retries survive 2 failures
    Given the destination answers 503 2 times on GET /api/v4/metadata
    When I run gitlab-sync with "--retry 2"
    Then the exit code is 0
    And the destination received 3 GET requests on "/api/v4/metadata"
    And the destination contains the project "mirror/app"

  Scenario: 2 retries do not survive 3 failures
    Given the destination answers 503 3 times on GET /api/v4/metadata
    When I run gitlab-sync with "--retry 2"
    Then the exit code is 1
    And the destination received 3 GET requests on "/api/v4/metadata"
    And the destination contains no project "mirror/app"

  Scenario: -1 retries as long as it takes
    Given the destination answers 503 8 times on GET /api/v4/metadata
    When I run gitlab-sync with "--retry -1"
    Then the exit code is 0
    And the destination received 9 GET requests on "/api/v4/metadata"

  Scenario: 0 is refused
    When I run gitlab-sync with "--retry 0"
    Then the exit code is 1
    And the output matches "retry count must be -1 \(no limit\) or strictly greater than 0"
    And the source received no GET request on "*"
    And the destination received no GET request on "*"

  Scenario: the default is 3 retries
    Given the destination answers 503 3 times on GET /api/v4/metadata
    When I run gitlab-sync
    Then the exit code is 0
    And the destination received 4 GET requests on "/api/v4/metadata"
