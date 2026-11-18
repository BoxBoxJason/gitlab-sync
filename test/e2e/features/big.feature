Feature: Big instances are never listed globally
  --source-big and --destination-big tell gitlab-sync not to list every project
  of an instance, which would take forever, but to ask for the mapped ones.

  Scenario: a small source is listed once, a big source is not
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the source received a GET request on "/api/v4/projects"

  Scenario: a big source gets its projects one by one
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync with "--source-big"
    Then the exit code is 0
    And the destination contains the project "mirror/app"
    And the source received no GET request on "/api/v4/projects"
    And the source received a GET request on "/api/v4/projects/team/app"

  Scenario: a big destination gets its projects one by one
    Given the destination has the project "mirror/app"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync with "--destination-big"
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination received no GET request on "/api/v4/projects"
    And the destination received a GET request on "/api/v4/projects/mirror/app"

  # BUG: a destination project that does not exist yet is expected, it is about to
  # be created. With --destination-big the GET answering 404 is reported as a
  # non-blocking error, so the run ends with exit code 2 although the project is
  # created and mirrored.
  @bug
  Scenario: a big destination without the project yet is not an error
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """
    When I run gitlab-sync with "--destination-big"
    Then the destination contains the project "mirror/app"
    And the exit code is 0

  Scenario: the 250 projects of a group of a big source are read page by page
    Given the destination has the group "mirror"
    And the source has 250 generated projects under "big"
    And the mirror mapping:
      """
      {"groups": {"big": {"destination_path": "mirror/big"}}}
      """
    When I run gitlab-sync with "--source-big --dry-run"
    Then the exit code is 0
    And the source received no GET request on "/api/v4/projects"
    And the source received 3 GET requests on "/api/v4/groups/*/projects"
    And the source received a GET request on "/api/v4/groups/*/projects" with the query "per_page=100"
    And the source received a GET request on "/api/v4/groups/*/projects" with the query "page=3"
    And the stdout matches "big/project-001 .*-> mirror/big/project-001"
    And the stdout matches "big/project-250 .*-> mirror/big/project-250"
