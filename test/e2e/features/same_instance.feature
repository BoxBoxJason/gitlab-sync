Feature: The source and the destination are the same place
  A project can be mirrored to another place of the group it comes from.

  Scenario: the project is copied next to itself
    Given the source has the project "team/app" with a commit on "main"
    And the source has the group "copies"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{source}/copies/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the source contains the project "team/app"
    And the source contains the project "copies/app"
    And the source project "copies/app" has the same refs as the source project "team/app"
