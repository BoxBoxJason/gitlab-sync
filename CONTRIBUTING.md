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

## Running e2e tests

The end-to-end tests run the real `gitlab-sync` binary against **fake GitLab instances**, never against a real GitLab. They are written in Gherkin and run by [godog](https://github.com/cucumber/godog).

```sh
make e2e
```

- The binary is built once with coverage instrumentation and every scenario runs it as a subprocess (so exit codes and fatal errors are real), with `--no-prompt`, stdin on `/dev/null` and a 60s timeout. Scenarios run 4 at a time.
- The coverage of the binary is written to `codequality/e2e-coverage.out`.
- `make test` does not run them (they are behind the `e2e` build tag), except for the unit tests of the fake GitLab.

How it is organized:

- `test/e2e/features/*.feature` holds the scenarios, one file per option or topic.
- `test/e2e/*.go` holds the step definitions. Read `steps.go` (the `Given` and `When` steps) and `steps_then.go` (the `Then` steps) for the vocabulary: set the starting state with readable steps (`the source has the project "team/app" with a commit on "main"`), give the mapping as a doc string (`the mirror mapping:`), then assert on the exit code, on regular expressions against the output, on the state of the instances and on the requests they received (`the destination received no write request`). `{source}`, `{destination}`, `{work}` and `{cache}` are replaced in the arguments.
- `test/e2e/gitlabmock` is the fake GitLab: an in-memory REST API plus git smart-HTTP, with a request journal and fault injection (`the destination answers 503 2 times on GET /api/v4/metadata`). It is **strict**: an endpoint it does not implement answers 501 and fails the scenario, naming the method and the path. Teach it the endpoint (and test it in `server_test.go`) rather than loosening it.

A scenario that exposes a bug of gitlab-sync is tagged `@bug`, with a comment above it describing the behavior seen. It is left out of `make e2e`, so that it can be fixed in its own pull request, which removes the tag. Run them on purpose with `E2E_TAGS=@bug make e2e` (any other tag expression works the same way, for example `E2E_TAGS=@wip`).

## Documentation

The documentation lives in the [`docs`](./docs) directory, as Markdown rendered by [Hugo](https://gohugo.io/) (extended edition) with the shared [hugo-theme](https://github.com/boxboxjason/hugo-theme), and is published to <https://boxboxjason.github.io/gitlab-sync/> from `main`.

- Change the documentation in the same pull request as the code it describes.
- `make docs-serve` serves the site locally with live reload, `make docs` builds it into `docs/public`.
- The [CLI reference](./docs/content/docs/cli-reference.md) page is generated from the CLI definition: run `make docs-generate` after changing a flag, and commit the result.
- `make test` fails when a flag, an environment variable or a mirror mapping option is not documented in the [usage](./docs/content/docs/usage.md) and [mapping](./docs/content/docs/mapping.md) pages, or when the generated page is stale.
- Look and feel (colors, footer, ...) belong to the shared theme, do not restyle a single site.

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
