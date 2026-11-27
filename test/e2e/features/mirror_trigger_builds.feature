Feature: mirror_trigger_builds
  A mirror does not run CI on the destination unless it is told to: the push of
  a Community Edition destination carries git push options saying so.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the source project "team/app" has a CI configuration

  Scenario Outline: the push runs no pipeline (<option>)
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination project "mirror/app" runs no pipeline

    Examples:
      | option                             |
      |                                    |
      | , "mirror_trigger_builds": false   |

  Scenario: the push runs a pipeline when asked to
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_trigger_builds": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination project "mirror/app" runs a pipeline

  @observed
  Scenario: the push skips the pipelines and the CI integrations
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination received the push option "ci.skip"
    And the destination received the push option "ci.no_pipeline"
    And the destination received the push option "integrations.skip_ci"

  @observed
  Scenario: a push that runs the builds carries no push option
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_trigger_builds": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination received no push option

  @observed
  Scenario: a group gives the option to the projects it mirrors
    Given the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team", "mirror_trigger_builds": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "team/app" has the same refs as the source project "team/app"
    And the destination received no push option
    And the destination project "team/app" runs a pipeline
