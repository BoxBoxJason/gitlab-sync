Feature: Being refused access
  A token that is not allowed to do something must produce a clear failure, and
  one forbidden project must not stop the others from being mirrored.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/a" with a commit on "main"
    And the source has the project "team/b" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {
        "{source}/team/a": {"destination_path": "{destination}/mirror/a"},
        "{source}/team/b": {"destination_path": "{destination}/mirror/b"}
      }}
      """

  @observed
  Scenario Outline: the source token is refused (<reason>)
    Given <token>
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "failed to get current user"
    And the output matches "<status>"
    And the destination received no write request

    Examples:
      | reason             | token                                              | status |
      | unknown token      | the source token is unknown to the instance        | 401    |
      | insufficient scope | the source token only has the scope "read_repository" | 403    |

  # GitLab hides what a user may not see (404), so the 403 is injected
  @observed
  Scenario: one project is forbidden, the others are mirrored
    Given the source answers 403 on GET /api/v4/projects/{source}/team/a
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the output matches "failed to retrieve project {source}/team/a"
    And the destination contains the project "mirror/b"
    And the destination project "mirror/b" has the same refs as the source project "team/b"
    And the destination contains no project "mirror/a"

  @observed
  Scenario: one group is forbidden
    Given the source has the project "other/c" with a commit on "main"
    And the source group "team" has an avatar
    And the source group "other" has an avatar
    And the mirror mapping:
      """
      {"groups": {
        "{source}/team": {"destination_path": "{destination}/team"},
        "{source}/other": {"destination_path": "{destination}/other"}
      }}
      """
    And the source answers 403 on GET /api/v4/groups/{source}/team
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the output matches "failed to retrieve group {source}/team"
    And the destination contains the project "other/c"
    And the destination contains no project "team/a"

  Scenario: the destination token cannot create projects
    Given the destination token only has the scope "read_api"
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "failed to create project {destination}/mirror/a"
    And the output matches "failed to create project {destination}/mirror/b"
    And the destination contains no project "mirror/a"

  Scenario: the destination token cannot create groups
    Given the source group "team" has an avatar
    And the destination token only has the scope "read_api"
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/mirror/team"}}}
      """
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "failed to create group"
    And the destination contains no group "mirror/team"
    And the destination contains no project "mirror/team/a"

  Scenario: the clone is forbidden for one project, the other is mirrored
    Given gitlab-sync runs on the source as a guest of the private project "team/a"
    When I run gitlab-sync
    Then the exit code is not 0
    And the output matches "failed to mirror project repository {source}/team/a"
    And the destination project "mirror/b" has the same refs as the source project "team/b"
    And the destination project "mirror/a" does not have the same refs as the source project "team/a"

  # The default branch of the destination diverged: GitLab protects it against
  # forced pushes
  Scenario: the push is forbidden for one project, the other is mirrored
    Given the destination has the project "mirror/a" with a commit on "main"
    And the destination project "mirror/a" has a protected default branch
    When I run gitlab-sync
    Then the exit code is not 0
    And the output matches "failed to mirror project repository {source}/team/a"
    And the destination project "mirror/b" has the same refs as the source project "team/b"
    And the destination project "mirror/a" does not have the same refs as the source project "team/a"
