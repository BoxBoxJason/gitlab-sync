Feature: The source and the destination are the same instance
  A project can be mirrored to another place of the instance it comes from.

  Background:
    Given the source and the destination are the same GitLab instance
    And the source has the project "team/app" with a commit on "main"
    And the source has the group "copies"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "copies/app"}}}
      """

  Scenario Outline: the project is copied inside the instance (<edition>)
    Given the destination is <edition>
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "team/app"
    And the destination contains the project "copies/app"
    And <mirroring>

    Examples:
      | edition  | mirroring                                                                                            |
      | freemium | the destination project "copies/app" has the same refs as the source project "team/app"              |
      | premium  | the destination project "copies/app" is a pull mirror of the source project "team/app"               |
