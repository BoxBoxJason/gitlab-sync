Feature: --cache-dir and --cache-max-age
  The clones of the source repositories can be kept between runs, so that the
  next run only fetches what changed. It only concerns the clone and push
  mirroring.

  Background:
    Given the destination has the group "mirror"
    And the source has the project "team/app" with a commit on "main"
    And the source project "team/app" has a commit on "main"
    And the source project "team/app" has a commit on "feature"
    And the mirror mapping:
      """
      {"projects": {"team/app": {"destination_path": "mirror/app"}}}
      """

  Scenario: a second run fetches less thanks to the cache
    Given the destination is freemium
    When I run gitlab-sync with "--cache-dir {cache}"
    And I run gitlab-sync with "--cache-dir {cache}"
    Then the exit code is 0
    And the file "{cache}" exists
    And run 2 downloaded less from the source than run 1
    And the destination project "mirror/app" has the same refs as the source project "team/app"

  Scenario: without the cache, every run fetches everything
    Given the destination is freemium
    When I run gitlab-sync
    And I run gitlab-sync
    Then the exit code is 0
    And run 2 downloaded as much from the source as run 1
    And "{cache}" does not exist

  Scenario: a premium destination does not use the cache
    Given the destination is premium
    When I run gitlab-sync with "--cache-dir {cache}"
    Then the exit code is 0
    And the destination project "mirror/app" is a pull mirror of the source project "team/app"
    And "{cache}" does not exist

  Scenario: a cache entry older than --cache-max-age is evicted
    Given the destination is freemium
    When I run gitlab-sync with "--cache-dir {cache}"
    And I run gitlab-sync with "--cache-dir {cache} --cache-max-age 1ns"
    Then the exit code is 0
    And run 2 downloaded as much from the source as run 1
    And the stderr matches "Removing an expired cache entry"
