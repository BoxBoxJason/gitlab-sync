Feature: ci_cd_catalog
  The destination project can be published in the CI/CD catalog (which needs
  GitLab 19.3 on the destination).

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"

  Scenario Outline: the project is added to the catalog (<edition>)
    Given the destination is <edition>
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "ci_cd_catalog": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" is in the CI/CD catalog

    Examples:
      | edition  |
      | premium  |
      | freemium |

  Scenario Outline: the project stays out of the catalog (<option>)
    Given the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" is not in the CI/CD catalog

    Examples:
      | option                    |
      |                           |
      | , "ci_cd_catalog": false  |

  Scenario: a project that is already in the catalog is not added again
    Given the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "ci_cd_catalog": true}}}
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
      {"groups": {"team": {"destination_path": "team", "ci_cd_catalog": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "team/app" is in the CI/CD catalog

  Scenario: a destination that refuses the catalog is reported
    Given the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "ci_cd_catalog": true}}}
      """
    And the destination answers 400 on PUT /api/v4/projects/*
    When I run gitlab-sync
    Then the exit code is not 0
    And the output matches "failed to add project mirror/app to CI/CD catalog"
