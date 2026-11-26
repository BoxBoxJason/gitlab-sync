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
        "groups": {"{source}/team": {"destination_path": "{destination}/mirror/team"}},
        "projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_releases": true}}
      }
      """

  @observed
  Scenario: nothing is written to the destination
    When I run gitlab-sync with "--dry-run"
    Then the exit code is 0
    And the destination received no write request
    And the source received no write request
    And the destination contains no project "mirror/app"
    And the destination contains no group "mirror/team"

  Scenario: the planned actions are listed
    When I run gitlab-sync with "--dry-run"
    Then the exit code is 0
    And the output matches "Groups that will be created"
    And the stdout matches "{source}/team .*\(source gitlab\) -> {destination}/mirror/team"
    And the output matches "Projects that will be created"
    And the stdout matches "{source}/team/app .*\(source gitlab\) -> {destination}/mirror/app"
    And the output matches "Dry run completed"
    And the stdout matches "Release v1 will be created in .*/{destination}/mirror/app"
