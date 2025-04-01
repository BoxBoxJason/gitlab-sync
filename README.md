# gitlab-sync

![gitlab-sync logo](./assets/gitlab-sync.png)

(golang) CLI tool to synchronize GitLab projects and groups between two GitLab instances.
It is designed to be used in a CI/CD pipeline to automate the process of keeping two GitLab instances in sync.

## Features

- Synchronize projects / groups between two GitLab instances
- Enable Pull Mirroring for projects (requires GitLab Premium)
- Can add projects to CI/CD catalog
- Full copy of the project (description, icon, topics,...). Can also copy issues

## Installation

### Docker

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

### Compile from source

To compile the CLI from source, you need to have Go installed on your machine.

1. Clone the repository: `git clone https://github.com/boxboxjason/gitlab-sync.git`
2. Change to the project directory: `cd gitlab-sync`
3. Build the CLI: `go build -o bin/gitlab-sync cmd/main.go`
4. The binary will be created in the `bin` directory.
5. Make sure the binary is executable: `chmod +x bin/gitlab-sync`
6. You can run the CLI from the `bin` directory: `./bin/gitlab-sync`
7. Optionally, you can move the binary to a directory in your `PATH` for easier access: `mv bin/gitlab-sync /usr/local/bin/`

## Usage

The CLI requires no dependencies and can be run directly. It is available as a single binary executable.

The mirroring configuration can be passed by either command line arguments or environment variables.
If mandatory arguments are not provided, the program will prompt for them.

| Argument | Environment Variable equivalent | Mandatory | Description |
|----------|-------------------------------|-----------|-------------|
| `--help` or `-h` | N/A | No | Show help message and exit |
| `--version` or `-V` | N/A | No | Show version information and exit |
| `--verbose` or `-v` | N/A | No | Enable verbose output |
| `--dry-run` | N/A | No | Perform a dry run without making any changes |
| `--source-url` | `SOURCE_GITLAB_URL` | Yes | URL of the source GitLab instance |
| `--source-token` | `SOURCE_GITLAB_TOKEN` | No | Access token for the source GitLab instance |
| `--destination-url` | `DESTINATION_GITLAB_URL` | Yes | URL of the destination GitLab instance |
| `--destination-token` | `DESTINATION_GITLAB_TOKEN` | Yes | Access token for the destination GitLab instance |
| `--mirror-mapping` | `MIRROR_MAPPING` | Yes | Path to a JSON file containing the mirror mapping |

## Example

```bash
gitlab-sync \
  --source-url https://gitlab.example.com \
  --source-token <source_gitlab_token> \
  --destination-url https://mycompany.example.com \
  --destination-token <destination_gitlab_token> \
  --mirror-mapping /path/to/mirror.json
```
