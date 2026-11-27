Feature: The destination is missing or unreachable

  Background:
    Given the source has the project "team/a" with a commit on "main"

  @observed
  Scenario: the destination cannot be reached
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/a": {"destination_path": "{destination}/mirror/a"}}}
      """
    And the destination cannot be reached
    When I run gitlab-sync with "--retry 1"
    Then the exit code is 1
    And the output matches "failed to get current user"
    And the source received no write request

  Scenario: the missing parent groups of the destination project are created
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/a": {"destination_path": "{destination}/missing/a"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the output matches "Creating missing parent group"
    And the destination contains the group "missing"
    And the destination contains the project "missing/a"
    And the destination project "missing/a" has the same refs as the source project "team/a"

  Scenario: the destination group of a mapped group is created when it is missing
    Given the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the group "team"
    And the destination contains the project "team/a"
    And the destination project "team/a" has the same refs as the source project "team/a"

  @observed
  Scenario: a second run creates nothing
    Given the destination has the group "mirror"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/a": {"destination_path": "{destination}/mirror/a"}}}
      """
    When I run gitlab-sync
    And I run gitlab-sync
    Then the exit code is 0
    And the destination received no POST request on "/api/v4/projects"
    And the destination received no POST request on "/api/v4/groups"
    And the destination contains the project "mirror/a"

  # Used to be a bug ("parent group not found"): the missing parents of a mapped
  # group are now looked up on the destination.
  Scenario: a group is mapped below an existing destination group
    Given the destination has the group "mirror"
    And the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/mirror/team"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "mirror/team/a"
