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

The end-to-end tests run the real `gitlab-sync` binary against a **real GitLab Community Edition**, never against a model of it. They are written in Gherkin and run by [godog](https://github.com/cucumber/godog).

```sh
make e2e
```

- `make e2e` starts a minimal GitLab CE container (`test/e2e/gitlab/compose.yaml`: only the services the tool uses, about 9 GB of RAM, 2 to 5 minutes to boot), creates an admin token (`test/e2e/gitlab/bootstrap.sh`), runs the scenarios and removes the container. It needs Docker with the compose plugin (override the command with `COMPOSE`, for example `COMPOSE=podman-compose`). One instance serves the whole run: the source and the destination are two groups of it per scenario, so scenarios never touch each other's projects and run in parallel (8 at a time, set `E2E_CONCURRENCY` to change it). A scenario removes its groups when it ends. One tagged `@alone` runs after the others, by itself: use it for those that create many projects.
- To use a throwaway instance you already have, set `GITLAB_E2E_URL` and `GITLAB_E2E_TOKEN` (an administrator token): the scenarios create groups, projects and users in it. The users and one empty group per run are left behind. Without these variables the scenarios are skipped.
- The binary is built once with coverage instrumentation and every scenario runs it as a subprocess (so exit codes and fatal errors are real), with `--no-prompt`, stdin on `/dev/null` and a timeout.
- Every scenario is listed with its result; the details of a failed scenario (exit code, output) are printed after the list. A JUnit report is written to `codequality/e2e-tests.xml` and the coverage of the binary to `codequality/e2e-coverage.out`.
- `make test` does not run them (they are behind the `e2e` build tag).

How it is organized:

- `test/e2e/features/*.feature` holds the scenarios, one file per option or topic.
- `test/e2e/*.go` holds the step definitions: `steps_given*.go` seeds the instance through the GitLab API, `steps_then*.go` and `steps_releases.go` check it through the same API, `steps_accounts.go` creates the users and tokens of the scenarios that need restricted ones, and `world*.go` runs the binary. Paths are relative to the group of the role (`the source project "app"`). `{source}` and `{destination}` are replaced by the group paths (in the mirror mapping, the arguments, the output patterns and the request paths), `{source_url}`, `{destination_url}`, `{work}` and `{cache}` by what their names say.
- A scenario tagged `@observed` has a proxy between the binary and the instance (`proxy.go`). It is for what the API cannot show: the requests made (`the destination received no write request`), the git push options, the bytes fetched, the tokens and User-Agent sent. It also fails requests on demand (`the destination answers 503 2 times on GET /api/v4/metadata`) for what a healthy instance never does. Every other request is answered by the real instance. Prefer the real behavior of GitLab (a scoped token, a protected branch) to an injected failure.
- GitLab Community Edition has no pull mirroring, so the premium behavior is only covered by the unit tests.
- `test/e2e/gitlab` is the GitLab CE configuration. Bump the image there deliberately, and do not re-enable a service unless a scenario needs it: the CI runner has limited resources.

A scenario that exposes a bug of gitlab-sync is tagged `@bug`, with a comment above it describing the behavior seen. It is left out of `make e2e`, so that it can be fixed in its own pull request, which removes the tag. Run them on purpose with `E2E_TAGS=@bug make e2e` (any other tag expression works the same way).

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
