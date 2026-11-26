Feature: Big instances are never listed globally
  --source-big and --destination-big tell gitlab-sync not to list every project
  of an instance, which would take forever, but to ask for the mapped ones.

  @observed
  Scenario: a small source is listed once
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the source received a GET request on "/api/v4/projects"

  @observed
  Scenario: a big source gets its projects one by one
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """
    When I run gitlab-sync with "--source-big"
    Then the exit code is 0
    And the destination contains the project "mirror/app"
    And the source received no GET request on "/api/v4/projects"
    And the source received a GET request on "/api/v4/projects/{source}/team/app"

  @observed
  Scenario: a big destination gets its projects one by one
    Given the destination has the project "mirror/app"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """
    When I run gitlab-sync with "--destination-big"
    Then the exit code is 0
    And the destination project "mirror/app" has the same refs as the source project "team/app"
    And the destination received no GET request on "/api/v4/projects"
    And the destination received a GET request on "/api/v4/projects/{destination}/mirror/app"

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
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"}}}
      """
    When I run gitlab-sync with "--destination-big"
    Then the destination contains the project "mirror/app"
    And the exit code is 0

  @observed @alone
  Scenario: the projects of a group of a big source are read page by page
    Given the destination has the group "mirror"
    And the source has 101 projects under "big"
    And the mirror mapping:
      """
      {"groups": {"{source}/big": {"destination_path": "{destination}/mirror/big"}}}
      """
    When I run gitlab-sync with "--source-big --dry-run"
    Then the exit code is 0
    And the source received no GET request on "/api/v4/projects"
    And the source received 2 GET requests on "/api/v4/groups/*/projects"
    And the source received a GET request on "/api/v4/groups/*/projects" with the query "per_page=100"
    And the source received a GET request on "/api/v4/groups/*/projects" with the query "page=2"
    And the stdout matches "{source}/big/project-001 .*-> {destination}/mirror/big/project-001"
    And the stdout matches "{source}/big/project-101 .*-> {destination}/mirror/big/project-101"
