Feature: Choosing how the destination is mirrored
  A Community Edition destination cannot pull the repositories itself:
  gitlab-sync pushes them. -f / --destination-force-freemium asks for the push
  whatever the instance says, and wins over -p / --destination-force-premium.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """

  # GitLab CE has no license endpoint: it answers 404
  Scenario: a Community Edition destination is pushed to
    When I run gitlab-sync
    Then the exit code is 0
    And the output matches "license endpoint not found"
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination project "mirror/app" is not a pull mirror

  Scenario Outline: <flags> pushes to the destination
    When I run gitlab-sync with "<flags>"
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination project "mirror/app" is not a pull mirror

    Examples:
      | flags                        |
      | -f                           |
      | --destination-force-freemium |
      | -p -f                        |
