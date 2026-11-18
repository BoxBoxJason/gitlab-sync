Feature: Mirroring projects
  A project of the mapping is created on the destination, filled with the
  repository of the source and kept in sync with it.

  Background:
    Given the destination has the group "mirror"

  Scenario Outline: a project is mirrored with its attributes (<edition>)
    Given the destination is <edition>
    And the source has the project "team/app" with:
      | description    | A small application |
      | topics         | go, cli             |
      | default branch | develop             |
    And the source project "team/app" has a commit on "develop"
    And the source project "team/app" has a commit on "feature"
    And the source project "team/app" has the tag "v1" on "develop"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the description "A small application"
    And the destination project "mirror/app" has the topics "go, cli"
    And the destination project "mirror/app" has the default branch "develop"
    And <repository>

    Examples:
      | edition  | repository                                                                                    |
      | premium  | the destination project "mirror/app" is a pull mirror of the source project "team/app"        |
      | freemium | the destination project "mirror/app" has the same refs as the source project "team/app"       |

  Scenario: the avatar of a project is copied
    Given the source has the project "team/app" with a commit on "main"
    And the source project "team/app" has an avatar
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the avatar of the source project "team/app"

  Scenario: several projects are mirrored in one run
    Given the source has the project "team/a" with a commit on "main"
    And the source has the project "team/b" with a commit on "main"
    And the source has the project "other/c" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {
        "team/a": {"destination_path": "mirror/a"},
        "team/b": {"destination_path": "mirror/b"},
        "other/c": {"destination_path": "mirror/c"}
      }}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/a" has the same refs as the source project "team/a"
    And the destination project "mirror/b" has the same refs as the source project "team/b"
    And the destination project "mirror/c" has the same refs as the source project "other/c"

  Scenario: a project that is not in the mapping is left alone
    Given the source has the project "team/a" with a commit on "main"
    And the source has the project "team/not-mapped" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/a": {"destination_path": "mirror/a"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "mirror/a"
    And the destination contains no project "mirror/not-mapped"

  Scenario: what changed on the source is applied by the next run
    Given the destination is freemium
    And the source has the project "team/app" with a commit on "main"
    And the source project "team/app" has a commit on "old-branch"
    And the source project "team/app" has the tag "v1" on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync
    And the source project "team/app" has a commit on "main"
    And the source project "team/app" has a commit on "new-branch"
    And the source project "team/app" now has the description "Now documented"
    And the source project "team/app" no longer has the branch "old-branch"
    And the source project "team/app" no longer has the tag "v1"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination project "mirror/app" contains no ref "refs/heads/old-branch"
    And the destination project "mirror/app" contains no ref "refs/tags/v1"
    And the destination project "mirror/app" contains the ref "refs/heads/new-branch"
    And the destination project "mirror/app" has the description "Now documented"

  # BUG: syncing the attributes of a project compares the visibility *pointers*
  # (copyOptions.Visibility != new(string(destinationProject.Visibility))), which
  # are never equal, so every run edits every destination project even when it
  # is already in sync.
  @bug
  Scenario: a run on an up-to-date destination does not edit the project
    Given the destination is freemium
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync
    And I run gitlab-sync
    Then the exit code is 0
    And the destination received no PUT request on "/api/v4/projects/*"
