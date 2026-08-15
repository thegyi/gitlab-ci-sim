# gitlab-ci-sim

A local GitLab CI pipeline simulator. Parse `.gitlab-ci.yml`, resolve
configuration, and execute jobs in Docker containers on your machine.

## Features

- Parse `.gitlab-ci.yml` with stages, jobs, variables, services, triggers
- Resolve `extends:`, `include:` (local/remote/project), `rules:`, `only:`/`except:`
- Execute jobs in Docker or Podman containers via a pluggable runtime
- Artifact and cache passing between jobs
- Variable engine seeded from local git state, with masking and CLI overrides
- Dry-run mode and pipeline graph (`gitlab-ci-sim graph`)
- Watch mode to re-run on config changes
- Colored terminal output
- Config linting/validation

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

## Usage

### Run the full pipeline

```bash
gitlab-ci-sim run
```

### Run specific jobs

```bash
gitlab-ci-sim run build_job test_job
```

### Dry-run (show what would execute)

```bash
gitlab-ci-sim run --dry-run
```

### Override variables

```bash
gitlab-ci-sim run -v CI_COMMIT_BRANCH=feature -v DEPLOY_ENV=staging
```

Mask sensitive values so they are redacted from the output:

```bash
gitlab-ci-sim run -v AD_PASSWORD=secret,masked -v GITLAB_PASSWORD=secret,masked
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

### Lint/validate configuration

```bash
gitlab-ci-sim lint
gitlab-ci-sim lint -f path/to/custom.yml
```

### Simulate a different branch

```bash
gitlab-ci-sim run --branch main
```

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

## Roadmap

### Implemented
- [x] Parse `.gitlab-ci.yml` (stages, jobs, scripts, images, services, triggers)
- [x] Variable engine from local git state, CLI overrides, and masking
- [x] `extends:` / `!reference` resolution
- [x] `include:` (local, remote, project)
- [x] `rules:` / `only:` / `except:` evaluation
- [x] Real Docker / Podman / fake container execution via pluggable runtime
- [x] `services:` support (linked containers with network aliases)
- [x] `artifacts:` and `cache:` passing between jobs
- [x] `needs:` DAG execution (parallel across stages when dependencies are met)
- [x] Dry-run mode and pipeline graph (`gitlab-ci-sim graph`)
- [x] Watch mode (re-run on config changes)
- [x] Colored terminal output
- [x] Config linting/validation

### Planned
- [ ] `workflow:` rules
- [ ] `parallel:` / `matrix:` expansion
- [ ] `when: manual` / `when: delayed` support
- [ ] Real downstream `trigger:` via GitLab API
- [ ] Graceful shutdown on `SIGINT`/`SIGTERM`
- [ ] Interactive job selection
- [ ] File-loaded variables (`.env`)
- [ ] More predefined CI variables and richer linting

## Notes and limitations

- `trigger:` jobs are recognised but **not executed locally**. They are reported as passed so the local simulation can continue, but they do not create a real downstream GitLab pipeline. Use the GitLab API or a real runner to trigger downstream pipelines.
- Some advanced GitLab features are not yet implemented (see Roadmap below).

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
