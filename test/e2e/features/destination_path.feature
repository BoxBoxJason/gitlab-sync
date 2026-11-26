Feature: destination_path
  destination_path is where a project or group lands on the destination. It can
  move it to another namespace, never rename it: its last element is the one of
  the source.

  Background:
    Given the source has the read-only project "team/app" with a commit on "main"

  Scenario: a project is moved to another namespace
    Given the destination has the group "archive/2026"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/archive/2026/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "archive/2026/app" has the same refs as the source project "team/app"
    And the destination contains no project "team/app"

  @observed
  Scenario: a project cannot be mirrored to the root of the destination
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "invalid project destination path \(must be in a namespace\): app"
    And the destination received no write request

  @observed
  Scenario Outline: an invalid mapping is refused before anything is done (<problem>)
    Given the mirror mapping:
      """
      <mapping>
      """
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "<message>"
    And the source received no GET request on "*"
    And the destination received no GET request on "*"

    Examples:
      | problem           | mapping                                                                                                         | message                                       |
      | renamed project   | {"projects": {"team/app": {"destination_path": "mirror/renamed"}}}                                              | same base name                                |
      | renamed group     | {"groups": {"team": {"destination_path": "mirror/other"}}}                                                      | same base name                                |
      | no destination    | {"projects": {"team/app": {}}}                                                                                  | invalid \(empty\) string in project mapping   |
      | trailing slash    | {"projects": {"team/app": {"destination_path": "mirror/app/"}}}                                                 | must not start or end with /                  |
      | duplicated target | {"projects": {"team/app": {"destination_path": "mirror/app"}, "other/app": {"destination_path": "mirror/app"}}} | duplicate destination path                    |
      | empty mapping     | {"projects": {}, "groups": {}}                                                                                  | no projects or groups defined                 |
      | not json          | this is not json                                                                                                | failed to decode mirror mapping file          |

  Scenario: the mapping file must exist
    When I run gitlab-sync with "--mirror-mapping {work}/missing.json"
    Then the exit code is 1
    And the output matches "failed to open mirror mapping file"
