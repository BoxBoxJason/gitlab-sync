Feature: The destination does not have the project yet

  Scenario: a missing destination project is created
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "mirror/app"
    And the destination project "mirror/app" has the same refs as the source project "team/app"
