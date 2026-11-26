Feature: Connecting to the two instances
  --source-url, --source-token, --destination-url and --destination-token tell
  where the projects come from, where they go, and who gitlab-sync is.

  Background:
    Given the destination has the group "mirror"
    And the source has the read-only project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """

  @observed
  Scenario: each instance is reached at its URL with its own token
    Given gitlab-sync uses a different account on each instance
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "mirror/app"
    And the source received a GET request on "/api/v4/user"
    And the destination received a GET request on "/api/v4/user"
    And every request to the source was authenticated with the source token
    And every request to the destination was authenticated with the destination token

  @observed
  Scenario Outline: a token the <instance> does not know fails the run
    Given the <instance> token is unknown to the instance
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "401"
    And the destination received no write request

    Examples:
      | instance    |
      | source      |
      | destination |
