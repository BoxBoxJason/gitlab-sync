Feature: mirror_releases
  The releases of the source project are created on the destination project.
  They are matched by tag, so a release is never created twice.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the source project "team/app" has the tag "v1" on "main"
    And the source project "team/app" has the release "v1"

  Scenario Outline: a release is copied (<edition>)
    Given the destination is <edition>
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_releases": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the release "v1"
    And the destination project "mirror/app" release "v1" has the name "Release v1"
    And the destination project "mirror/app" release "v1" has the description "notes of v1"

    Examples:
      | edition  |
      | premium  |
      | freemium |

  Scenario: a second run only creates the new releases
    Given the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_releases": true}}}
      """
    When I run gitlab-sync
    And the source project "team/app" has a commit on "main"
    And the source project "team/app" has the tag "v2" on "main"
    And the source project "team/app" has the release "v2"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the release "v1"
    And the destination project "mirror/app" contains the release "v2"
    And the destination received 1 POST request on "/api/v4/projects/*/releases"

  Scenario Outline: the releases are not copied (<option>)
    Given the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains no release "v1"
    And the source received no GET request on "/api/v4/projects/*/releases"

    Examples:
      | option                       |
      |                              |
      | , "mirror_releases": false   |

  Scenario: the links of a release are not copied without mirror_release_assets
    Given the source project "team/app" release "v1" has the link "docs" to "https://example.com/docs.pdf"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_releases": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" release "v1" has 0 links

  # Premium, because on a freemium destination pushing the repository deletes the
  # tags the source does not have, and GitLab deletes their releases with them.
  Scenario: a release the source does not have is left alone without mirror_release_assets
    Given the destination is premium
    And the destination has the project "mirror/app"
    And the destination project "mirror/app" has the release "legacy"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_releases": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the release "legacy"
    And the destination project "mirror/app" contains the release "v1"

  Scenario: a group gives the option to the projects it mirrors
    Given the source group "team" has an avatar
    And the mirror mapping:
      """
      {"groups": {"team": {"destination_path": "team", "mirror_releases": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "team/app" contains the release "v1"
