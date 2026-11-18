Feature: The version of the destination
  Pull mirroring needs GitLab 17.6 or later (INSTANCE_SEMVER_THRESHOLD), on top
  of a Premium license.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    And the destination is premium

  Scenario Outline: a premium destination in version <version>
    Given the destination version is "<version>"
    When I run gitlab-sync
    Then the exit code is 0
    And <mirroring>

    Examples:
      | version  | mirroring                                                                                    |
      | 17.5.9   | the destination project "mirror/app" has the same refs as the source project "team/app"      |
      | 17.6.0   | the destination project "mirror/app" is a pull mirror of the source project "team/app"       |
      | 17.6.1   | the destination project "mirror/app" is a pull mirror of the source project "team/app"       |
      | 18.2.0   | the destination project "mirror/app" is a pull mirror of the source project "team/app"       |
      | 17.10.0-pre | the destination project "mirror/app" is a pull mirror of the source project "team/app"    |

  Scenario: a version that cannot be parsed stops the run
    Given the destination version is "not-a-version"
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "failed to parse GitLab version"
    And the destination received no write request

  Scenario: a version cannot be fixed by forcing premium
    Given the destination version is "17.5.0"
    When I run gitlab-sync with "-p"
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
