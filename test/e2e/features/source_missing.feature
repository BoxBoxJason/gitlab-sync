Feature: The source does not have what the mapping asks for

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/a" with a commit on "main"

  Scenario: the source cannot be reached
    Given the mirror mapping:
      """
      {"projects": {"team/a": {"destination_path": "mirror/a"}}}
      """
    And the source cannot be reached
    When I run gitlab-sync with "--retry 1"
    Then the exit code is 1
    And the output matches "failed to get current user"
    And the destination received no write request

  Scenario: a mapped project answers 404, the others are mirrored
    Given the mirror mapping:
      """
      {"projects": {
        "team/a": {"destination_path": "mirror/a"},
        "team/gone": {"destination_path": "mirror/gone"}
      }}
      """
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the output matches "failed to retrieve project team/gone"
    And the destination project "mirror/a" has the same refs as the source project "team/a"
    And the destination contains no project "mirror/gone"

  Scenario: a mapped group answers 404
    Given the mirror mapping:
      """
      {"groups": {"nowhere": {"destination_path": "mirror/nowhere"}}}
      """
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the destination received no POST request on "/api/v4/projects"

  Scenario: a mapped project is neither on the source nor in a mapped group
    Given the mirror mapping:
      """
      {"projects": {
        "team/a": {"destination_path": "mirror/a"},
        "team/gone": {"destination_path": "mirror/gone"}
      }}
      """
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "project team/gone not found in source GitLab instance"
    And the destination project "mirror/a" has the same refs as the source project "team/a"
    And the destination contains no project "mirror/gone"
