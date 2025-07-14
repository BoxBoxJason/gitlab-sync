# Contributing to GitLab Sync

Thank you for considering contributing to GitLab Sync! We welcome contributions from the community. Here are some guidelines to help you get started.

## Prerequisites

Before contributing, please ensure you have the following prerequisites:

1. Clone the repository: `git clone https://github.com/boxboxjason/gitlab-sync.git`
2. Install [Go](https://golang.org/doc/install) (see [go.mod](./go.mod) for the required version).
3. Install [Make](https://www.gnu.org/software/make/) (if not already installed).
4. Install [golangci-lint](https://golangci-lint.run/) and [gosec](https://github.com/securego/gosec) for static analysis and security checks.
5. Install [gotestsum](https://github.com/gotestyourself/gotestsum) to format test results.
6. Optional: Install [dependency-check](https://owasp.org/www-project-dependency-check/) for dependency vulnerability checks.
7. Optional: Install [Docker](https://www.docker.com/) or [Podman](https://podman.io/) for building the Docker image.

## Development Workflow

1. **Create a new branch** for your changes: `git checkout -b feature/my-feature`
2. **Make your changes** and ensure they are well-tested.
3. **Run the tests** to ensure everything is working correctly: `make test`
4. **Run the linter** to check for code quality: `make lint`
5. **Run security checks** to ensure there are no vulnerabilities: `make dependency-check`
6. Submit a **pull request** with a clear description of your changes and why they are needed.

## Code Style

We follow the standard Go code style. Please ensure your code adheres to the following:

- Use `gofmt` to format your code.
- Use meaningful variable and function names.
- Write clear and concise comments where necessary.
- Ensure your code is well-tested with unit tests.
- Avoid unnecessary complexity; keep your code simple and readable.
- Use `golangci-lint` to check for common issues and code smells.
- Use `gosec` to check for security issues in your code.
- Use `gotestsum` to format test results for better readability.
- Use `dependency-check` to check for known vulnerabilities in your dependencies.
