Feature: Choosing how the destination is mirrored
  A destination that is Premium and recent enough (17.6 or later) pulls the
  repositories itself. Any other destination gets them pushed by gitlab-sync.
  -p / --destination-force-premium and -f / --destination-force-freemium
  override what the instance says it is.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """

  Scenario: a premium destination pulls
    Given the destination is premium
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" is a pull mirror of the source project "team/app"
    And the destination received no POST request on "/mirror/app.git/git-receive-pack"

  Scenario: a freemium destination is pushed to
    Given the destination is freemium
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination project "mirror/app" is not a pull mirror

  Scenario Outline: <flag> overrides the license of the destination
    Given the destination license plan is "<plan>"
    When I run gitlab-sync with "<flag>"
    Then the exit code is 0
    And <outcome>

    Examples:
      | flag                           | plan    | outcome                                                                                       |
      | -f                             | premium | the destination project "mirror/app" has the same refs as the source project "team/app"      |
      | --destination-force-freemium   | premium | the destination project "mirror/app" has the same refs as the source project "team/app"      |
      | -p                             | free    | the destination project "mirror/app" is a pull mirror of the source project "team/app"        |
      | --destination-force-premium    | free    | the destination project "mirror/app" is a pull mirror of the source project "team/app"        |

  Scenario: forcing premium on an instance without a license endpoint is enough
    Given the destination has no license endpoint
    When I run gitlab-sync with "-p"
    Then the exit code is 0
    And the destination project "mirror/app" is a pull mirror of the source project "team/app"

  Scenario: an instance without a license endpoint cannot be told apart unless forced
    Given the destination has no license endpoint
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "failed to check if destination GitLab instance is premium"

  Scenario: when both are forced, freemium wins
    Given the destination is premium
    When I run gitlab-sync with "-p -f"
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination project "mirror/app" is not a pull mirror
