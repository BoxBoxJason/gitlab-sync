Feature: Connecting to the two instances
  --source-url, --source-token, --destination-url and --destination-token tell
  where the projects come from, where they go, and who gitlab-sync is.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """

  Scenario Outline: each instance is reached at its URL with its own token
    Given the destination is <edition>
    And the source only accepts the token "source-token"
    And the destination only accepts the token "destination-token"
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "mirror/app"
    And the source received a GET request on "/api/v4/user"
    And the destination received a GET request on "/api/v4/user"

    Examples:
      | edition  |
      | premium  |
      | freemium |

  Scenario: the tokens of the flags are the ones sent, not the other way around
    Given the source only accepts the token "destination-token"
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "401"
    And the destination received no write request
