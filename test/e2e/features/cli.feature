Feature: The command line
  Everything that does not need a GitLab instance.

  Scenario: --version prints the version and the Go version
    When I run gitlab-sync with the bare arguments "--version"
    Then the exit code is 0
    And the stdout matches "\S+ \(go: "

  Scenario: --help describes the flags
    When I run gitlab-sync with the bare arguments "--help"
    Then the exit code is 0
    And the stdout matches "Usage:"
    And the stdout matches "--source-url"
    And the stdout matches "--mirror-mapping"

  Scenario Outline: completion generates a script for <shell>
    When I run gitlab-sync with the bare arguments "completion <shell>"
    Then the exit code is 0
    And the stdout matches "(?s).{100,}"
    And the stdout matches "gitlab-sync"

    Examples:
      | shell      |
      | bash       |
      | zsh        |
      | fish       |
      | powershell |

  Scenario: completion refuses an unknown shell
    When I run gitlab-sync with the bare arguments "completion nushell"
    Then the exit code is 1
    And the stdout does not match "(?s).{100,}"
    And the stderr matches "invalid argument"

  Scenario: an unknown flag is refused
    When I run gitlab-sync with the bare arguments "--no-such-flag"
    Then the exit code is 1
    And the stderr matches "unknown flag"
