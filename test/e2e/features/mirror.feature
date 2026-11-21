Feature: Mirroring against a real GitLab
  The binary runs against a real GitLab Community Edition instance, as source
  and destination, so that what it relies on is the behavior of GitLab itself
  and not of a model of it.

  Scenario: a project is mirrored with its repository and its attributes
    Given the source project "app" exists with the description "A small application" and the topics "go, cli"
    And the source project "app" has a commit on "main"
    And the source project "app" has a commit on "feature"
    And the source project "app" has the tag "v1" on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/app": {"destination_path": "{destination}/app"}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "app" has the description "A small application"
    And the destination project "app" has the topics "cli, go"
    And the destination project "app" has the same branches and tags as the source project "app"

  Scenario: the issues and the releases are mirrored
    Given the source project "app" exists
    And the source project "app" has a commit on "main"
    And the source project "app" has the tag "v1" on "main"
    And the source project "app" has the release "v1"
    And the source project "app" has the opened issue "It crashes"
    And the source project "app" has the closed issue "It was slow"
    And the mirror mapping:
      """
      {"projects": {"{source}/app": {"destination_path": "{destination}/app", "mirror_issues": true, "mirror_releases": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "app" has the opened issue "It crashes"
    And the destination project "app" has the closed issue "It was slow"
    And the destination project "app" has 2 issues
    And the destination project "app" has the release "v1"

  Scenario: a second run brings the changes of the source and copies nothing twice
    Given the source project "app" exists with the description "First" and the topics ""
    And the source project "app" has a commit on "main"
    And the source project "app" has the opened issue "It crashes"
    And the mirror mapping:
      """
      {"projects": {"{source}/app": {"destination_path": "{destination}/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    Given the source project "app" has a commit on "main"
    And the source project "app" has the tag "v2" on "main"
    And the source project "app" now has the description "Second"
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "app" has the description "Second"
    And the destination project "app" has the same branches and tags as the source project "app"
    And the destination project "app" has 1 issue

  Scenario: a dry run changes nothing
    Given the source project "app" exists
    And the source project "app" has a commit on "main"
    And the mirror mapping:
      """
      {"projects": {"{source}/app": {"destination_path": "{destination}/app"}}}
      """
    When I run gitlab-sync with "--dry-run"
    Then the exit code is 0
    And the destination has no project "app"
