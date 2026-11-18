Feature: The destination does not have the project yet

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """

  Scenario Outline: a missing destination project is created
    Given the destination is <edition>
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "mirror/app"
    And <mirroring>

    Examples:
      | edition  | mirroring                                                                                       |
      | freemium | the destination project "mirror/app" has the same refs as the source project "team/app"         |
      | premium  | the destination project "mirror/app" is a pull mirror of the source project "team/app"          |
