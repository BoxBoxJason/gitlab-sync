Feature: mirror_release_assets
  With mirror_releases, the links of the releases are copied too, along with the
  files they point to when those are on the source instance. The destination
  releases are then kept in sync: what the source no longer has is deleted.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the source project "team/app" has the tag "v1" on "main"
    And the source project "team/app" has the release "v1"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_releases": true, "mirror_release_assets": true}}}
      """

  Scenario: a link to somewhere else is copied unchanged
    Given the source project "team/app" release "v1" has the link "docs" to "https://example.com/docs.pdf"
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" release "v1" contains the link "docs" to "https://example.com/docs.pdf"
    And the destination project "mirror/app" has 0 generic package files

  # The URL the instance gives to an upload is a web route: only the observed
  # scenarios, whose source and destination URLs differ, can tell the copy apart
  @observed
  Scenario: a file of the source instance is copied to the package registry of the destination
    Given the source project "team/app" has the uploaded file "note.txt" with the content "release notes"
    And the source project "team/app" release "v1" has the link "notes" to the uploaded file "note.txt"
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the generic package file "release-assets/v1/note.txt" with the content "release notes"
    And the destination project "mirror/app" release "v1" contains the link "notes" to the package file "release-assets/v1/note.txt"

  @observed
  Scenario: a generic package file is copied under the same name and version
    Given the source project "team/app" has the generic package file "tool/1.0.0/tool.bin" with the content "binary v1"
    And the source project "team/app" release "v1" has the link "tool" to the package file "tool/1.0.0/tool.bin" at "/bin/tool"
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the generic package file "tool/1.0.0/tool.bin" with the content "binary v1"
    And the destination project "mirror/app" release "v1" contains the link "tool" to the package file "tool/1.0.0/tool.bin"

  @observed
  Scenario: a generic package file that did not change is not copied again
    Given the source project "team/app" has the generic package file "tool/1.0.0/tool.bin" with the content "binary v1"
    And the source project "team/app" release "v1" has the link "tool" to the package file "tool/1.0.0/tool.bin" at "/bin/tool"
    When I run gitlab-sync
    And I run gitlab-sync
    Then the exit code is 0
    And the destination received no PUT request on "/api/v4/projects/*/packages/generic/*"
    And the destination project "mirror/app" has 1 generic package file

  @observed
  Scenario: a generic package file that changed is copied again
    Given the source project "team/app" has the generic package file "tool/1.0.0/tool.bin" with the content "binary v1"
    And the source project "team/app" release "v1" has the link "tool" to the package file "tool/1.0.0/tool.bin" at "/bin/tool"
    When I run gitlab-sync
    And the source project "team/app" has the generic package file "tool/1.0.0/tool.bin" with the content "binary v2, longer"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination received 1 PUT request on "/api/v4/projects/*/packages/generic/*"
    And the destination project "mirror/app" contains the generic package file "tool/1.0.0/tool.bin" with the content "binary v2, longer"

  Scenario: a link the source release no longer has is deleted with its file
    Given the source project "team/app" has the generic package file "tool/1.0.0/tool.bin" with the content "binary v1"
    And the source project "team/app" release "v1" has the link "tool" to the package file "tool/1.0.0/tool.bin" at "/bin/tool"
    And the source project "team/app" release "v1" has the link "docs" to "https://example.com/docs.pdf"
    When I run gitlab-sync
    And the source project "team/app" release "v1" no longer has the link "tool"
    And the source project "team/app" no longer has the generic package file "tool/1.0.0/tool.bin"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" release "v1" has 1 link
    And the destination project "mirror/app" release "v1" contains the link "docs" to "https://example.com/docs.pdf"
    And the destination project "mirror/app" contains no generic package file "tool/1.0.0/tool.bin"

  Scenario: a file another release still links to is kept
    Given the source project "team/app" has the generic package file "tool/1.0.0/tool.bin" with the content "binary v1"
    And the source project "team/app" has the tag "v2" on "main"
    And the source project "team/app" has the release "v2"
    And the source project "team/app" release "v1" has the link "tool" to the package file "tool/1.0.0/tool.bin" at "/bin/tool"
    And the source project "team/app" release "v2" has the link "tool" to the package file "tool/1.0.0/tool.bin" at "/bin/tool"
    When I run gitlab-sync
    And the source project "team/app" release "v1" no longer has the link "tool"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" release "v1" has 0 links
    And the destination project "mirror/app" release "v2" has 1 link
    And the destination project "mirror/app" contains the generic package file "tool/1.0.0/tool.bin" with the content "binary v1"

  Scenario: a file that no longer exists on the source is removed from the destination
    Given the source project "team/app" has the generic package file "tool/1.0.0/tool.bin" with the content "binary v1"
    And the source project "team/app" release "v1" has the link "tool" to the package file "tool/1.0.0/tool.bin" at "/bin/tool"
    When I run gitlab-sync
    And the source project "team/app" no longer has the generic package file "tool/1.0.0/tool.bin"
    And I run gitlab-sync
    Then the destination project "mirror/app" contains no generic package file "tool/1.0.0/tool.bin"

  @observed
  Scenario: a release the source no longer has is deleted with its files
    Given the source project "team/app" has the uploaded file "note.txt" with the content "release notes"
    And the source project "team/app" release "v1" has the link "notes" to the uploaded file "note.txt"
    When I run gitlab-sync
    And the source project "team/app" no longer has the release "v1"
    And the source project "team/app" no longer has the tag "v1"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains no release "v1"
    And the destination project "mirror/app" contains no generic package file "release-assets/v1/note.txt"

  # The tag exists on both sides, so that the push does not delete it (GitLab
  # would delete its release along with it): this is the deletion of the app.
  @observed
  Scenario: a destination release the source never had is deleted
    Given the source project "team/app" has the tag "legacy" on "main"
    When I run gitlab-sync
    And the destination project "mirror/app" has the release "legacy"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination received a DELETE request on "/api/v4/projects/*/releases/legacy"
    And the destination project "mirror/app" contains no release "legacy"
    And the destination project "mirror/app" contains the release "v1"

  Scenario: a missing file of the source does not stop the other assets
    Given the source project "team/app" release "v1" has the link "gone" to "{source_url}/uploads/-/system/gone.txt"
    And the source project "team/app" release "v1" has the link "docs" to "https://example.com/docs.pdf"
    When I run gitlab-sync
    Then the exit code is not 0
    And the output matches "failed to mirror asset gone of release v1"
    And the destination project "mirror/app" release "v1" contains the link "docs" to "https://example.com/docs.pdf"

  Scenario: mirror_release_assets alone does nothing
    Given the source project "team/app" release "v1" has the link "docs" to "https://example.com/docs.pdf"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_release_assets": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains no release "v1"

  @observed
  Scenario: a dry run says what would be done to the assets
    Given the source project "team/app" has the uploaded file "note.txt" with the content "release notes"
    And the source project "team/app" release "v1" has the link "notes" to the uploaded file "note.txt"
    And the source project "team/app" release "v1" has the link "docs" to "https://example.com/docs.pdf"
    When I run gitlab-sync with "--dry-run"
    Then the exit code is 0
    And the stdout matches "Asset notes: file copied to the destination package registry as release-assets/v1/note.txt"
    And the stdout matches "Asset docs: external link copied unchanged"
    And the destination received no write request
