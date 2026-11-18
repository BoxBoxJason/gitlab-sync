Feature: visibility
  The visibility option decides how visible the destination project is,
  whatever the visibility of the source. It defaults to public.

  Background:
    Given the destination has the group "mirror"

  Scenario Outline: the destination project is <visibility> (<edition>)
    Given the destination is <edition>
    And the source has the project "team/app" with:
      | visibility | private |
    And the source project "team/app" has a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "visibility": "<visibility>"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the visibility "<visibility>"

    Examples:
      | edition  | visibility |
      | freemium | public     |
      | freemium | internal   |
      | freemium | private    |
      | premium  | internal   |

  Scenario: without the option the destination project is public
    Given the source has the project "team/app" with:
      | visibility | private |
    And the source project "team/app" has a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the visibility "public"

  Scenario: changing the option changes the destination project
    Given the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "visibility": "internal"}}}
      """
    When I run gitlab-sync
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "visibility": "private"}}}
      """
    And I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the visibility "private"

  Scenario: a group gives its visibility to the projects it mirrors
    Given the source has the project "team/app" with a commit on "main"
    And the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"team": {"destination_path": "team", "visibility": "private"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "team/app" has the visibility "private"

  Scenario: an unknown visibility is refused
    Given the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "visibility": "secret"}}}
      """
    When I run gitlab-sync
    Then the exit code is 1
    And the output matches "invalid project visibility: secret"
    And the destination received no write request

  # BUG: when a group is created, its attributes are synchronized by
  # updateGroupFromSource, which is called with the roles swapped (see the groups
  # feature). The visibility of the mapping is then applied to the group of the
  # SOURCE instance, which is modified although it must only be read.
  @bug
  Scenario: mirroring a group never changes the source
    Given the source has the project "team/app" with a commit on "main"
    And the source group "team" is public
    And the mirror mapping:
      """
      {"groups": {"team": {"destination_path": "team", "visibility": "private"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the source received no write request
    And the source group "team" is still public
