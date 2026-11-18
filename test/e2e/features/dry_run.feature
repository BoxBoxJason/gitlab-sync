Feature: --dry-run
  A dry run reads both instances and says what a real run would do, without
  changing anything.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the source project "team/app" has the release "v1"
    And the mirror mapping:
      """
      {
        "groups": {"team": {"destination_path": "mirror/team"}},
        "projects": {"team/app": {"destination_path": "mirror/app", "mirror_releases": true}}
      }
      """

  Scenario Outline: nothing is written to the destination
    Given the destination is <edition>
    When I run gitlab-sync with "--dry-run"
    Then the exit code is 0
    And the destination received no write request
    And the source received no write request
    And the destination contains no project "mirror/app"
    And the destination contains no group "mirror/team"

    Examples:
      | edition  |
      | premium  |
      | freemium |

  Scenario: the planned actions are listed
    When I run gitlab-sync with "--dry-run"
    Then the exit code is 0
    And the output matches "Groups that will be created"
    And the stdout matches "team .*\(source gitlab\) -> mirror/team"
    And the output matches "Projects that will be created"
    And the stdout matches "team/app .*\(source gitlab\) -> mirror/app"
    And the output matches "Dry run completed"
    And the stdout matches "Release v1 will be created in .*/mirror/app"
