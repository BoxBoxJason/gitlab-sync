Feature: ci_cd_catalog
  The destination project can be published in the CI/CD catalog (which needs
  GitLab 19.3 on the destination).

  Background:
    Given the destination has the group "mirror"
    # The catalog only accepts a project with a description
    And the source project "team/app" exists with the description "A component" and the topics ""
    And the source project "team/app" has a commit on "main"

  Scenario: the project is added to the catalog
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "ci_cd_catalog": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" is in the CI/CD catalog

  Scenario Outline: the project stays out of the catalog (<option>)
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" is not in the CI/CD catalog

    Examples:
      | option                    |
      |                           |
      | , "ci_cd_catalog": false  |

  # BUG: the project listing of GitLab does not tell whether a project is in the
  # catalog (only getting a project does), and gitlab-sync reads the destination
  # projects from listings, so every run adds them to the catalog again.
  @bug @observed
  Scenario: a project that is already in the catalog is not added again
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "ci_cd_catalog": true}}}
      """
    When I run gitlab-sync
    And I run gitlab-sync
    Then the exit code is 0
    And the destination received no PUT request on "/api/v4/projects/*" with the body "cicd_catalog_enabled"
    And the destination project "mirror/app" is in the CI/CD catalog

  Scenario: a group gives the option to the projects it mirrors
    Given the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team", "ci_cd_catalog": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "team/app" is in the CI/CD catalog

  Scenario: a destination that refuses the catalog is reported
    Given the source project "team/app" now has the description ""
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "ci_cd_catalog": true}}}
      """
    When I run gitlab-sync
    Then the exit code is not 0
    And the output matches "failed to add project {destination}/mirror/app to CI/CD catalog"
