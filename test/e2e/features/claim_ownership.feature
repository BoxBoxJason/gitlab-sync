Feature: claim_ownership
  The user of the destination token can become an owner of what is mirrored.
  GitLab only lets a user reconfigure the mirror of a project it is a
  maintainer of, so this is reasserted on every run.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    # A member of nothing, so that every membership comes from gitlab-sync
    And gitlab-sync runs on the destination as an administrator who is a member of nothing

  Scenario: the user owns the project
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is an owner of the destination project "mirror/app"

  @observed
  Scenario Outline: the user does not own the project (<option>)
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is not a direct member of the destination project "mirror/app"
    And the destination received no POST request on "/api/v4/projects/*/members"

    Examples:
      | option                       |
      |                              |
      | , "claim_ownership": false   |

  @observed
  Scenario: ownership is reasserted on every run
    Given the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    And I run gitlab-sync
    Then the exit code is 0
    And the destination received 1 PUT request on "/api/v4/projects/*/members/*"
    And the destination received no POST request on "/api/v4/projects/*/members"
    And the authenticated user is an owner of the destination project "mirror/app"

  @observed
  Scenario: a user who already has a lower access level is promoted
    Given the destination has the project "mirror/app"
    And the destination project "mirror/app" has the authenticated user as developer
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is an owner of the destination project "mirror/app"
    And the destination received no POST request on "/api/v4/projects/*/members"

  # BUG: the groups read from the source are stored in the mapping under their
  # own path, which replaces the entry of the mapping with options that do not
  # carry claim_ownership (StoreGroup). The option is lost before the groups are
  # created, so the user never becomes an owner of a mapped group. (GitLab makes
  # the creator of a group its owner, so only an existing group can show it.)
  @bug
  Scenario: a group is claimed
    Given the destination has the group "team"
    And the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team", "claim_ownership": true}}}
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
      {"groups": {"{source}/team": {"destination_path": "{destination}/team", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the authenticated user is an owner of the destination project "team/app"

  Scenario: failing to claim the project does not stop the mirroring
    # A maintainer cannot make anybody an owner
    Given gitlab-sync runs on the destination as a maintainer of the group "mirror"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "claim_ownership": true}}}
      """
    When I run gitlab-sync
    Then the destination project "mirror/app" has the same refs as the source project "team/app"
    And the output matches "Failed to claim ownership of project"
