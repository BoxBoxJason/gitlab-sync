Feature: The source does not have what the mapping asks for

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/a" with a commit on "main"

  @observed
  Scenario: the source cannot be reached
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/a": {"destination_path": "{destination}/mirror/a"}}}
      """
    And the source cannot be reached
    When I run gitlab-sync with "--retry 1"
    Then the exit code is 1
    And the output matches "failed to get current user"
    And the destination received no write request

  Scenario: a mapped project does not exist, the others are mirrored
    Given the mirror mapping:
      """
      {"projects": {
        "{source}/team/a": {"destination_path": "{destination}/mirror/a"},
        "{source}/team/gone": {"destination_path": "{destination}/mirror/gone"}
      }}
      """
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the output matches "failed to retrieve project {source}/team/gone"
    And the destination project "mirror/a" has the same refs as the source project "team/a"
    And the destination contains no project "mirror/gone"

  @observed
  Scenario: a mapped group does not exist
    Given the mirror mapping:
      """
      {"groups": {"{source}/nowhere": {"destination_path": "{destination}/mirror/nowhere"}}}
      """
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the output matches "failed to retrieve group {source}/nowhere"
    And the destination received no POST request on "/api/v4/projects"
    And the destination received no POST request on "/api/v4/groups"

  Scenario: a mapped group does not exist, the other mapped group is mirrored
    Given the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {
        "{source}/nowhere": {"destination_path": "{destination}/nowhere"},
        "{source}/team": {"destination_path": "{destination}/team"}
      }}
      """
    When I run gitlab-sync with "--source-big"
    Then the exit code is not 0
    And the output matches "failed to retrieve group {source}/nowhere"
    And the destination project "team/a" has the same refs as the source project "team/a"
    And the destination contains no group "nowhere"

  Scenario: a mapped project is neither on the source nor in a mapped group
    Given the mirror mapping:
      """
      {"projects": {
        "{source}/team/a": {"destination_path": "{destination}/mirror/a"},
        "{source}/team/gone": {"destination_path": "{destination}/mirror/gone"}
      }}
      """
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "project {source}/team/gone not found in source GitLab instance"
    And the destination project "mirror/a" has the same refs as the source project "team/a"
    And the destination contains no project "mirror/gone"
