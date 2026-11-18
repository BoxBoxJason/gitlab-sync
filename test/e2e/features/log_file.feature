Feature: --log-file
  The logs are also written to a file, whose directories are created.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """

  Scenario: the log file is created with its missing parent directories
    When I run gitlab-sync with "--log-file {work}/deep/er/sync.log"
    Then the exit code is 0
    And the file "{work}/deep/er/sync.log" exists
    And the file "{work}/deep/er/sync.log" matches "Starting GitLab mirroring process"
    And the file "{work}/deep/er/sync.log" matches "Completed project mirroring"
    And the file "{work}/deep/er/sync.log" matches "\"level\":\"INFO\""
    And the file "{work}/deep/er/sync.log" matches "Mirroring completed successfully"

  Scenario: without the flag, no file is written
    When I run gitlab-sync
    Then the exit code is 0
    And "{work}/sync.log" does not exist
    And the stderr matches "Starting GitLab mirroring process"

  Scenario: the logs still go to the standard error when a file is given
    When I run gitlab-sync with "--log-file {work}/sync.log"
    Then the exit code is 0
    And the stderr matches "Starting GitLab mirroring process"
