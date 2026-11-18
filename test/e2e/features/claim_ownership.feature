Feature: claim_ownership
  The user of the destination token can become an owner of what is mirrored.
  GitLab only lets a user reconfigure the mirror of a project it is a
  maintainer of, so this is reasserted on every run.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"

  Scenario Outline: the user owns the project (<edition>)
    Given the destination is <edition>
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is an owner of the destination project "mirror/app"

    Examples:
      | edition  |
      | premium  |
      | freemium |

  Scenario Outline: the user does not own the project (<option>)
    Given the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is not a member of the destination project "mirror/app"
    And the destination received no POST request on "/api/v4/projects/*/members"

    Examples:
      | option                       |
      |                              |
      | , "claim_ownership": false   |

  Scenario: ownership is reasserted on every run
    Given the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    And I run gitlab-sync
    Then the exit code is 0
    And the destination received 1 PUT request on "/api/v4/projects/*/members/*"
    And the destination received no POST request on "/api/v4/projects/*/members"
    And the authenticated user is an owner of the destination project "mirror/app"

  Scenario: a user who already has a lower access level is promoted
    Given the destination has the project "mirror/app"
    And the destination project "mirror/app" has the authenticated user as developer
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is an owner of the destination project "mirror/app"
    And the destination received no POST request on "/api/v4/projects/*/members"

  # BUG: the groups read from the source are stored in the mapping under their
  # own path, which replaces the entry of the mapping with options that do not
  # carry claim_ownership (StoreGroup). The option is lost before the groups are
  # created, so the user never becomes an owner of a mapped group.
  @bug
  Scenario: a group is claimed when it is created
    Given the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"team": {"destination_path": "team", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is an owner of the destination group "team"

  # BUG: same replacement as above, which races with the projects reading the
  # options of their group: the user is an owner of the projects of the group only
  # when they are read before the group is stored (most runs: they are not).
  @bug
  Scenario: a project of a group is claimed when the group asks for it
    Given the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"team": {"destination_path": "team", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is an owner of the destination project "team/app"

  Scenario: failing to claim the project does not stop the mirroring
    Given the destination answers 403 on POST /api/v4/projects/*/members
    And the destination answers 403 on PUT /api/v4/projects/*/members/*
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the destination project "mirror/app" has the same refs as the source project "team/app"
    And the output matches "Failed to claim ownership of project"
