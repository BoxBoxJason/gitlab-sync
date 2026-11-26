Feature: Configuration against a real GitLab
  Configuration options are checked by observing the real GitLab instance and
  the files written by the binary, rather than by inspecting a fake server.

  Scenario: a mapped project is created below a new destination namespace
    Given the source project "app" exists
    And the source project "app" has a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/app": {"destination_path": "{destination}/archive/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "archive/app" has the same branches and tags as the source project "app"

  Scenario Outline: the requested project visibility is applied
    Given the source project "app" exists
    And the source project "app" has a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/app": {"destination_path": "{destination}/app", "visibility": "<visibility>"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "app" has the visibility "<visibility>"

    Examples:
      | visibility |
      | public     |
      | internal   |
      | private    |

  Scenario: an invalid destination path is rejected
    Given the mirror mapping:
      """
      {"projects": {"{source}/app": {"destination_path": "app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "invalid project destination path \(must be in a namespace\): app"

  Scenario: the log file is written as well as standard error
    Given the source project "app" exists
    And the source project "app" has a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/app": {"destination_path": "{destination}/app"}}}
      """
    When I run gitlab-sync with "--log-file {work}/logs/sync.log"
    Then the exit code is 0
    And the file "{work}/logs/sync.log" exists
    And the file "{work}/logs/sync.log" matches "Mirroring completed successfully"
    And the stderr matches "Starting GitLab mirroring process"
