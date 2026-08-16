# gitlab-ci-sim

A local GitLab CI pipeline simulator. Parse `.gitlab-ci.yml`, resolve
includes/extends/rules, and execute jobs in Docker or Podman containers on your
local machine.

## Features

### Core YAML support

- `stages:`, `variables:`, `workflow: rules:`, `default:`
- Jobs with `script:`, `before_script:`, `after_script:`, `image:`, `stage:`
- `extends:`, `!reference`, `include:` (local/remote/project)
- `rules:`, `only:`/ `except:`, `needs:`, `dependencies:`
- `retry:` (scalar and full mapping), `parallel:` (scalar and matrix)
- `services:` with network aliases and health checks
- `artifacts:`, `cache:`, `trigger:`, `when:` (including `manual` and `delayed`)

### Execution

- Docker, Podman, or `fake` container runtime
- DAG execution via `needs:`
- Artifact and cache passing between jobs
- Graceful shutdown on `SIGINT` / `SIGTERM`
- Real GitLab downstream pipeline triggering (opt-in via `--trigger-mode=gitlab`)

### CLI and UX

- `gitlab-ci-sim run`, `graph`, `lint`
- `--dry-run`, `--watch`, `--list`, `--interactive`
- `--branch`, `--tags`, `--runtime`, `--env-file`, `--manual`
- Colored terminal output with live container logs
- Masked variables (`-v KEY=VALUE,masked`) are redacted from output

## Install

```bash
go install github.com/thegyi/gitlab-ci-sim@latest
```

Or build from source:

```bash
git clone https://github.com/thegyi/gitlab-ci-sim.git
cd gitlab-ci-sim
go build -o gitlab-ci-sim .
```

## Dependencies

- Go 1.22 or later
- Docker Engine (accessible via the `docker` CLI)
- Git
- Go modules used by this project:
  - `github.com/spf13/cobra` — CLI framework
  - `gopkg.in/yaml.v3` — YAML parsing
  - `github.com/Knetic/govaluate` — `rules:` `if:` expression evaluation

## CLI overview

| Command | Description |
|---------|-------------|
| `gitlab-ci-sim run` | Execute the pipeline (or selected jobs). |
| `gitlab-ci-sim run --dry-run` | Print what would run without executing. |
| `gitlab-ci-sim run --list` | List the jobs that would be selected. |
| `gitlab-ci-sim run --interactive` | Pick jobs interactively. |
| `gitlab-ci-sim graph` | Show the pipeline DAG. |
| `gitlab-ci-sim lint` | Validate the YAML and pipeline. |

### `run` flags

| Flag | Description |
|------|-------------|
| `-f, --file` | Path to `.gitlab-ci.yml` (default: `.gitlab-ci.yml`). |
| `-v, --variable` | Override a variable as `KEY=VALUE` or `KEY=VALUE,masked`. |
| `-e, --env-file` | Load variables from a `.env` file. |
| `--branch` | Simulate a specific branch. |
| `--runtime` | `docker`, `podman`, or `fake` (default: `docker`). |
| `--tags` | Run only jobs with one of these tags (untagged jobs always run). |
| `--manual` | Treat `when: manual` jobs as runnable. |
| `--strict-variables` | Abort jobs that reference undefined/empty variables (default: `true`; use `false` to disable). |
| `--trigger-mode` | `local` (default) or `gitlab` for real downstream triggers. |
| `--watch` | Re-run when `.gitlab-ci.yml` changes. |
| `--dry-run` | Preview without running. |
| `--list` | List selected jobs and exit. |
| `--interactive` | Select jobs from a numbered list. |

## Usage examples

### Run the full pipeline

```bash
gitlab-ci-sim run
```

### Run specific jobs

```bash
gitlab-ci-sim run build_job test_job
```

### Choose container runtime

```bash
gitlab-ci-sim run --runtime podman
```

### Override variables

```bash
gitlab-ci-sim run -v CI_COMMIT_BRANCH=feature -v DEPLOY_ENV=staging
```

Mask sensitive values so they are redacted from the output:

```bash
gitlab-ci-sim run -v AD_PASSWORD=secret,masked -v GITLAB_PASSWORD=secret,masked
```

### Load variables from a `.env` file

```bash
gitlab-ci-sim run --env-file=.env
```

### Run `trigger:` jobs against the real GitLab API

By default `trigger:` jobs are skipped locally. Use `--trigger-mode=gitlab` to
create the downstream pipeline:

```bash
gitlab-ci-sim run --trigger-mode=gitlab build_pr
```

This requires `CI_SERVER_URL` and either `GITLAB_TOKEN` (private token) or
`CI_JOB_TOKEN` to be set.

### List jobs before running

```bash
gitlab-ci-sim run --list
```

### Select jobs interactively

```bash
gitlab-ci-sim run --interactive
```

### Show the pipeline graph

```bash
gitlab-ci-sim graph
```

### Watch mode

Re-run the pipeline when `.gitlab-ci.yml` changes:

```bash
gitlab-ci-sim run --watch
```

### Filter by tags

Run only jobs tagged with `docker` or `linux` (jobs without any tags are also
included):

```bash
gitlab-ci-sim run --tags docker,linux
```

### Lint/validate configuration

```bash
gitlab-ci-sim lint
gitlab-ci-sim lint -f path/to/custom.yml
```

### Simulate a different branch

```bash
gitlab-ci-sim run --branch main
```

## Predefined CI variables

The simulator seeds the following variables from your local git state:

- `CI`, `GITLAB_CI`, `CI_SERVER`, `CI_SERVER_URL`, `CI_SERVER_HOST`
- `CI_COMMIT_BRANCH`, `CI_COMMIT_REF_NAME`, `CI_COMMIT_REF_SLUG`
- `CI_COMMIT_SHA`, `CI_COMMIT_SHORT_SHA`
- `CI_PIPELINE_ID`, `CI_PIPELINE_SOURCE`
- `CI_PROJECT_NAME`, `CI_PROJECT_PATH`, `CI_PROJECT_NAMESPACE`, `CI_PROJECT_ROOT_NAMESPACE`
- `CI_PROJECT_URL`, `CI_PROJECT_DIR`
- `CI_REPOSITORY_URL`, `CI_DEFAULT_BRANCH`
- `CI_REGISTRY`, `CI_REGISTRY_IMAGE`

## Project Structure

```
├── main.go                  # Entry point
├── cmd/                     # CLI commands (cobra)
│   ├── root.go              # Root command and global flags
│   ├── run.go               # `run` subcommand
│   ├── graph.go             # `graph` subcommand
│   └── lint.go              # `lint` subcommand
├── pkg/
│   ├── parser/              # YAML parser and config resolution
│   ├── pipeline/            # Pipeline builder (stages, DAG, rules)
│   ├── executor/            # Docker/Podman/fake runtime executor
│   ├── variables/           # CI variable engine
│   ├── artifacts/           # Artifact store between jobs
│   └── term/                # Terminal color helpers
└── README.md
```

## Feature support

| Feature | Status | Notes |
|---------|--------|-------|
| `stages`, `variables`, `workflow: rules`, `default:` | Supported | |
| Jobs (`script`, `before_script`, `after_script`) | Supported | |
| `image:` (string or `{ name, entrypoint }`) | Supported | |
| `extends:`, `!reference` | Supported | `extends:` accepts a single name or a list. |
| `include:` (local, remote, project, template, component) | Supported | Remote/project/component require network/Git. |
| `rules:` (if, changes, exists, when, variables) | Supported | Bare variable `if:` is treated as a boolean. |
| `only:` / `except:` | Supported | |
| `needs:` (incl. `optional`, `artifacts`) | Supported | |
| `dependencies:` | Supported | |
| `retry:` | Supported | Scalar and mapping forms. |
| `parallel:` | Supported | Scalar and matrix. |
| `services:` | Supported | With network aliases and health checks. |
| `artifacts:` | Supported | Paths and `reports: coverage_report`. |
| `cache:` | Supported | Key prefix, `key: { files }`, `untracked`, `when`, `policy`. |
| `trigger:` | Partial | Skipped locally by default; real trigger via `--trigger-mode=gitlab`. |
| `when:` (on_success, on_failure, always, manual, delayed) | Supported | |
| `allow_failure:` | Supported | Scalar, mapping and `exit_codes`. |
| `coverage:` regex | Supported | Sets `CI_JOB_COVERAGE`. |
| Masked variables | Supported | Redacted from output. |
| `tags:` | Supported | Untagged jobs always run. |
| Docker / Podman runtime | Supported | Requires the `docker` or `podman` CLI. |
| `fake` runtime | Supported | Fast test mode; runs `sh` on the host. |
| DAG execution | Supported | Based on `needs:` / `dependencies:`. |
| `pages:` | Not supported | GitLab Pages is a deployment feature with no local equivalent. |
| `environment:` | Not supported | No environment API to publish to. |
| `release:` | Not supported | No GitLab release API integration. |
| `resource_group:` | Not supported | Local runner has no concurrency control. |
| `interruptible:` | Not supported | No cross-pipeline cancellation. |
| `id_tokens:`, `secrets:` | Not supported | No Vault/CI ID token integration. |
| Docker-in-Docker / privileged containers | Not supported | |
| Kubernetes executor | Not supported | Only Docker/Podman/fake runtimes. |

## Limitations

- `trigger:` jobs are skipped locally by default. Use `--trigger-mode=gitlab` to
create the downstream pipeline via the GitLab API (requires `CI_SERVER_URL`
and `GITLAB_TOKEN` or `CI_JOB_TOKEN`).
- Some advanced GitLab features are intentionally out of scope for a local
simulator (see the **Not supported** entries in the table above).

## Development

```bash
# Run tests
go test ./...

# Build
go build -o gitlab-ci-sim .

# Run on a sample project
cd /path/to/project-with-gitlab-ci
gitlab-ci-sim run --dry-run
```

## License

MIT
