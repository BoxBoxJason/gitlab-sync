Feature: Running the binary
  The harness runs the real binary as a subprocess.

  Scenario: the version is printed
    When I run gitlab-sync with the bare arguments "--version"
    Then the exit code is 0
    And the stdout matches "\S+ \(go: "
