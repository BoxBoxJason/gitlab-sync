Feature: Being refused access (403)
  A token that is not allowed to do something must produce a clear failure, and
  one forbidden project must not stop the others from being mirrored.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/a" with a commit on "main"
    And the source has the project "team/b" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {
        "team/a": {"destination_path": "mirror/a"},
        "team/b": {"destination_path": "mirror/b"}
      }}
      """

  Scenario Outline: the source token is not valid (<status>)
    Given the source answers <status> on GET /api/v4/user
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "failed to get current user"
    And the destination received no write request

    Examples:
      | status |
      | 401    |
      | 403    |

  Scenario: the source does not know the token
    Given the source only accepts the token "another-token"
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "401"
    And the destination contains no project "mirror/a"

  Scenario: one project is forbidden, the others are mirrored
    Given the source answers 403 on GET /api/v4/projects/team/a
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the output matches "failed to retrieve project team/a"
    And the destination contains the project "mirror/b"
    And the destination project "mirror/b" has the same refs as the source project "team/b"
    And the destination contains no project "mirror/a"

  Scenario: one group is forbidden
    Given the source has the project "other/c" with a commit on "main"
    And the source group "team" has an avatar
    And the source group "other" has an avatar
    And the mirror mapping:
      """
      {"groups": {
        "team": {"destination_path": "team"},
        "other": {"destination_path": "other"}
      }}
      """
    And the source answers 403 on GET /api/v4/groups/team
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the destination contains the project "other/c"
    And the destination contains no project "team/a"

  Scenario: the destination token cannot create projects
    Given the destination answers 403 on POST /api/v4/projects
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "failed to create project mirror/a"
    And the output matches "failed to create project mirror/b"

  Scenario: the destination token cannot create groups
    Given the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"team": {"destination_path": "mirror/team"}}}
      """
    And the destination answers 403 on POST /api/v4/groups
    When I run gitlab-sync
    Then the exit code is 1
    And the destination contains no project "team/a"

  Scenario Outline: the git <operation> is forbidden for one project, the other is mirrored
    Given the destination is freemium
    And the <instance> forbids git <operation> of "<project>"
    When I run gitlab-sync
    Then the exit code is not 0
    And the output matches "failed to mirror project repository"
    And the destination project "mirror/b" has the same refs as the source project "team/b"
    And the destination project "mirror/a" has no refs

    Examples:
      | operation | instance    | project   |
      | clone     | source      | team/a    |
      | push      | destination | mirror/a  |
