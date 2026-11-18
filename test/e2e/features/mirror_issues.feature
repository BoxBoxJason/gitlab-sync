Feature: mirror_issues
  The issues of the source project are copied to the destination project.
  They are matched by title, so an issue is never copied twice.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"

  Scenario Outline: an issue is copied (<edition>)
    Given the destination is <edition>
    And the source project "team/app" has the opened issue "It crashes"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the opened issue "It crashes"
    And the destination project "mirror/app" has 1 issue

    Examples:
      | edition  |
      | premium  |
      | freemium |

  Scenario: a closed issue is copied and closed
    Given the source project "team/app" has the closed issue "It was slow"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the closed issue "It was slow"

  # BUG: a closed issue is closed in the destination by its number on the SOURCE
  # (CloseIssue uses issue.IID of the source issue). It only is the right one when
  # the destination numbers its issues like the source does: with an issue already
  # there, or with several issues created at the same time, another issue is
  # closed instead and the copy stays opened.
  @bug
  Scenario: closing an issue closes the copy of it
    Given the destination has the project "mirror/app"
    And the destination project "mirror/app" has the opened issue "Already there"
    And the source project "team/app" has the closed issue "It was slow"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" contains the opened issue "Already there"
    And the destination project "mirror/app" contains the closed issue "It was slow"

  Scenario: a second run copies only the new issues
    Given the source project "team/app" has the opened issue "It crashes"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    And the source project "team/app" has the opened issue "A new one"
    And I run gitlab-sync
    Then the exit code is 0
    And the destination project "mirror/app" has 2 issues
    And the destination project "mirror/app" contains the opened issue "A new one"
    And the destination received 1 POST request on "/api/v4/projects/*/issues"

  Scenario Outline: the issues are not copied (<option>)
    Given the source project "team/app" has the opened issue "It crashes"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"<option>}}}
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
      {"groups": {"team": {"destination_path": "team", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the destination project "team/app" has 1 issue

  Scenario: the issues are read in pages of 100
    Given the source project "team/app" has the opened issue "Issue 1"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app", "mirror_issues": true}}}
      """
    When I run gitlab-sync
    Then the exit code is 0
    And the source received a GET request on "/api/v4/projects/*/issues" with the query "per_page=100"
    And the destination project "mirror/app" has 1 issue
