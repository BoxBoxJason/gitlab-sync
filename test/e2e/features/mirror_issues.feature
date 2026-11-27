Feature: mirror_issues
  The issues of the source project are copied to the destination project.
  They are matched by title, so an issue is never copied twice.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"

  Scenario: an issue is copied
    Given the source project "team/app" has the opened issue "It crashes"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the opened issue "It crashes"
    And the destination project "mirror/app" has 1 issue

  Scenario: a closed issue is copied and closed
    Given the source project "team/app" has the closed issue "It was slow"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the closed issue "It was slow"

  Scenario: closing an issue closes the copy of it
    Given the destination has the project "mirror/app"
    And the destination project "mirror/app" has the opened issue "Already there"
    And the source project "team/app" has the closed issue "It was slow"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the opened issue "Already there"
    And the destination project "mirror/app" contains the closed issue "It was slow"

  @observed
  Scenario: a second run copies only the new issues
    Given the source project "team/app" has the opened issue "It crashes"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    And the source project "team/app" has the opened issue "A new one"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has 2 issues
    And the destination project "mirror/app" contains the opened issue "A new one"
    And the destination received 1 POST request on "/api/v4/projects/*/issues"

  @observed
  Scenario Outline: the issues are not copied (<option>)
    Given the source project "team/app" has the opened issue "It crashes"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app"<option>}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has 0 issues
    And the source received no GET request on "/api/v4/projects/*/issues"

    Examples:
      | option                    |
      |                           |
      | , "mirror_issues": false  |

  Scenario: a group gives the option to the projects it mirrors
    Given the source group "team" has an avatar
    And the source project "team/app" has the opened issue "It crashes"
    And the mirror mapping:
      """
      {"groups": {"{source}/team": {"destination_path": "{destination}/team", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "team/app" has 1 issue

  @observed
  Scenario: the issues are read in pages of 100
    Given the source project "team/app" has the opened issue "Issue 1"
    And the mirror mapping:
      """
      {"projects": {"{source}/team/app": {"destination_path": "{destination}/mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the source received a GET request on "/api/v4/projects/*/issues" with the query "per_page=100"
    And the destination project "mirror/app" has 1 issue
