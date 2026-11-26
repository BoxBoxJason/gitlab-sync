Feature: Mirroring groups
  A group of the mapping is created on the destination, along with its
  subgroups and all their projects.

  Background:
    Given the source has the project "team/a" with a commit on "main"
    And the source has the project "team/sub/b" with a commit on "main"
    And the source has the project "elsewhere/c" with a commit on "main"
    # Avatars are what keeps the group scenarios clear of the bug described at the
    # end of this file.
    And the source group "team" has an avatar
    And the source group "team/sub" has an avatar

  Scenario: a group is mirrored with its subgroups and projects
    Given the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the group "team"
    And the destination contains the group "team/sub"
    And the destination contains the project "team/a"
    And the destination contains the project "team/sub/b"
    And the destination contains no project "elsewhere/c"
    And the destination project "team/sub/b" has the same refs as the source project "team/sub/b"

  @observed
  Scenario: a group of a big source is read subgroup by subgroup
    Given the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team"}}}
      """
    When I run gitlab-sync with "--source-big"
    Then the exit code is 0
    And the source received no GET request on "/api/v4/projects"
    And the source received a GET request on "/api/v4/groups/*/subgroups"
    And the destination contains the project "team/sub/b"

  Scenario: a project mapped on its own wins over its group
    Given the destination has the group "other"
    And the mirror mapping:
      """
      {
        "groups": {"{source}/team": {"destination_path": "{destination}/team"}},
        "projects": {"{source}/team/a": {"destination_path": "{destination}/other/a"}}
      }
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "other/a"
    And the destination contains no project "team/a"
    And the destination contains the project "team/sub/b"

  Scenario: an archived project of a group is not mirrored
    Given the source has the archived project "team/old"
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "team/a"
    And the destination contains no project "team/old"

  @observed
  Scenario: existing destination groups are reused
    Given the destination has the group "team/sub"
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination received no POST request on "/api/v4/groups"
    And the destination contains the project "team/a"

  # BUG: when a group is created, its attributes and avatar are synchronized by
  # updateGroupFromSource, which is called with the roles swapped: it is a method
  # of the destination instance but is called on the source one, with the
  # destination group where the source group is expected. The avatar of the
  # source group is therefore never copied.
  @bug
  Scenario: the avatar of a group is copied
    Given the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination group "team" has the avatar of the source group "team"

  # BUG: same swap as above. A source group without an avatar makes the run
  # download the avatar of the (new, avatar-less) destination group from the
  # source instance, which answers 404: the run ends with exit code 2 although
  # nothing is wrong.
  @bug
  Scenario: a group without an avatar is mirrored without any error
    Given the mirror mapping:
      """
      {"groups": {"{source}/elsewhere": {"destination_path": "{destination}/elsewhere"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination contains the project "elsewhere/c"
