Feature: --user-agent
  Every request tells the instances who is calling.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """

  Scenario Outline: the API requests carry the given User-Agent
    Given the destination is <edition>
    When I run gitlab-sync with "--user-agent e2e-agent/1.2"
    Then the exit code is 0
    And the source received a GET request on "/api/v4/user"
    And the destination received a GET request on "/api/v4/user"
    And every API request to the source carried the User-Agent "^e2e-agent/1\.2$"
    And every API request to the destination carried the User-Agent "^e2e-agent/1\.2$"

    Examples:
      | edition  |
      | premium  |
      | freemium |

  # BUG: --user-agent is only applied to the GitLab API client. The git requests
  # (clone from the source, push to the destination) keep go-git's own
  # "go-git/5.x" User-Agent.
  @bug
  Scenario: the git requests carry the given User-Agent too
    Given the destination is freemium
    When I run gitlab-sync with "--user-agent e2e-agent/1.2"
    Then the exit code is 0
    And the source received a GET request on "/team/app.git/info/refs"
    And every request to the source carried the User-Agent "^e2e-agent/1\.2$"
    And every request to the destination carried the User-Agent "^e2e-agent/1\.2$"

  Scenario: the default User-Agent names the program and its version
    When I run gitlab-sync
    Then the exit code is 0
    And every API request to the source carried the User-Agent "^gitlab-sync/\S+$"
    And every API request to the destination carried the User-Agent "^gitlab-sync/\S+$"
