Feature: mirror_trigger_builds
  A mirror does not run CI on the destination unless it is told to. With a pull
  mirror it is the mirror_trigger_builds attribute of the project; with the push
  of a freemium destination it is a set of git push options.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"

  Scenario Outline: a pull mirror triggers builds <expected> (<option>)
    Given the destination is premium
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" mirror_trigger_builds is <expected>

    Examples:
      | option                             | expected |
      |                                    | false    |
      | , "mirror_trigger_builds": false   | false    |
      | , "mirror_trigger_builds": true    | true     |

  Scenario Outline: the push does not run CI unless asked to (<option>)
    Given the destination is freemium
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And <push>

    Examples:
      | option                             | push                                                    |
      |                                    | the destination received the push option "ci.skip"     |
      | , "mirror_trigger_builds": false   | the destination received the push option "ci.skip"     |
      | , "mirror_trigger_builds": true    | the destination received no push option                 |

  Scenario: the push skips the pipelines and the CI integrations
    Given the destination is freemium
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination received the push option "ci.skip"
    And the destination received the push option "ci.no_pipeline"
    And the destination received the push option "integrations.skip_ci"

  Scenario: a group gives the option to the projects it mirrors
    Given the destination is freemium
    And the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"team": {"destination_path": "team", "mirror_trigger_builds": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "team/app" has the same refs as the source project "team/app"
    And the destination received no push option
