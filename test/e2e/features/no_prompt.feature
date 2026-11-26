Feature: -n / --no-prompt
  Without the flag gitlab-sync asks for what is missing. With it, it refuses to
  guess and stops. The tests always run with it (stdin is /dev/null).

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """

  @observed
  Scenario Outline: a mandatory value that is missing fails the run
    When I run gitlab-sync without "<flag>"
    Then the exit code is 1
    And the output matches "Prompting is disabled"
    And the source received no GET request on "*"
    And the destination received no GET request on "*"

    Examples:
      | flag                |
      | --source-url        |
      | --destination-url   |
      | --destination-token |
      | --mirror-mapping    |

  Scenario: -n is the short form of --no-prompt
    When I run gitlab-sync with "-n" without "--no-prompt --source-url"
    Then the exit code is 1
    And the output matches "Prompting is disabled"

  Scenario: the source token is optional
    When I run gitlab-sync without "--source-token"
    Then the exit code is 0
    And the destination contains the project "mirror/app"
