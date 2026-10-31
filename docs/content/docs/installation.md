---
title: Installation
description: "Install gitlab-sync with the container image from GHCR, go install, or a build from source, and set up shell completion."
weight: 10
---

## Docker

You can run the CLI using Docker. It is available on GitHub Container Registry.

1. Pull the Docker image: `docker pull ghcr.io/boxboxjason/gitlab-sync:latest`
2. Run the Docker container with the required command line arguments / env variables (don't forget to mount the JSON file):

```bash
docker run --rm \
  -e SOURCE_GITLAB_URL=<source_gitlab_url> \
  -e SOURCE_GITLAB_TOKEN=<source_gitlab_token> \
  -e DESTINATION_GITLAB_URL=<destination_gitlab_url> \
  -e DESTINATION_GITLAB_TOKEN=<destination_gitlab_token> \
  -e MIRROR_MAPPING=/home/gitlab-sync/mirror.json \
  -v <my_mapping_json_file>:/home/gitlab-sync/mirror.json:ro,Z \
  ghcr.io/boxboxjason/gitlab-sync:latest
```

## Install with go install

You can install the latest published version directly with Go:

```bash
go install github.com/boxboxjason/gitlab-sync@latest
```

To install a specific version, use a semantic version tag:

```bash
go install github.com/boxboxjason/gitlab-sync@v1.2.3
```

By default, `go install` places the binary in `$(go env GOPATH)/bin`.
If this directory is not in your `PATH`, either run the binary with its full path:

```bash
"$(go env GOPATH)/bin/gitlab-sync" --version
```

Or add it to your shell profile:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Then run:

```bash
gitlab-sync --version
```

## Compile from source

To compile the CLI from source, you need to have Go installed on your machine.

1. Clone the repository: `git clone https://github.com/boxboxjason/gitlab-sync.git`
2. Change to the project directory: `cd gitlab-sync`
3. Build the CLI: `go build -o bin/gitlab-sync .`
4. The binary will be created in the `bin` directory.
5. Make sure the binary is executable: `chmod +x bin/gitlab-sync`
6. You can run the CLI from the `bin` directory: `./bin/gitlab-sync`
7. Optionally, you can move the binary to a directory in your `PATH` for easier access: `mv bin/gitlab-sync /usr/local/bin/`

## Shell autocomplete

The CLI provides shell completions, including completion for available flags on the main command.

Generate completion script:

```bash
gitlab-sync completion bash
gitlab-sync completion zsh
gitlab-sync completion fish
gitlab-sync completion powershell
```

Examples:

```bash
# Bash (current session)
source <(gitlab-sync completion bash)

# Zsh (save then source from ~/.zshrc)
gitlab-sync completion zsh > "${fpath[1]}/_gitlab-sync"

# Fish
gitlab-sync completion fish > ~/.config/fish/completions/gitlab-sync.fish
```
