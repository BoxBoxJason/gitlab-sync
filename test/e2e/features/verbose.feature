Feature: -v / --verbose
  DEBUG lines only show up when asked for.

  Background:
    Given the destination has the group "mirror"
    And the source has the read-only project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """

  Scenario Outline: <flag> prints the DEBUG lines
    When I run gitlab-sync with "<flag>"
    Then the exit code is 0
    And the stderr matches "\"level\":\"DEBUG\""

    Examples:
      | flag      |
      | -v        |
      | --verbose |

  Scenario: there is no DEBUG line without the flag
    When I run gitlab-sync
    Then the exit code is 0
    And the stderr matches "\"level\":\"INFO\""
    And the stderr does not match "\"level\":\"DEBUG\""
