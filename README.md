# gitlab-ci-sim

A local GitLab CI pipeline simulator. Parse `.gitlab-ci.yml`, resolve
configuration, and execute jobs in Docker containers on your machine.

## Features

- Parse `.gitlab-ci.yml` with stages, jobs, variables, services
- Resolve `extends:`, `include:` (planned), `rules:`
- Execute jobs in Docker containers
- Artifact passing between jobs
- Variable engine seeded from local git state
- Dry-run mode for pipeline visualization
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
│   └── lint.go              # `lint` subcommand
├── pkg/
│   ├── parser/              # YAML parser and config resolution
│   ├── pipeline/            # Pipeline builder (stages, DAG, rules)
│   ├── executor/            # Docker-based job executor
│   ├── variables/           # CI variable engine
│   └── artifacts/           # Artifact store between jobs
└── README.md
```

## Roadmap

### Phase 1 — MVP (current)
- [x] Parse `.gitlab-ci.yml` (stages, jobs, scripts, images)
- [x] Variable engine from local git state
- [x] Pipeline builder with stage ordering
- [x] Stub executor (prints commands)
- [x] Dry-run mode
- [x] Config linting
- [ ] Docker executor (real container execution)

### Phase 2 — Pipeline execution
- [ ] Real Docker container execution
- [ ] `services:` support (linked containers)
- [ ] `artifacts:` passing between jobs
- [ ] `cache:` support
- [ ] `needs:` DAG execution (parallel within stage)

### Phase 3 — Full resolution
- [ ] `include:` (local, remote, template, project)
- [ ] `extends:` / `!reference` resolution
- [ ] `rules:` evaluation (if/changes/exists)
- [ ] `parallel: matrix:` expansion
- [ ] `only:` / `except:` evaluation

### Phase 4 — UX
- [ ] Pipeline graph visualization (terminal)
- [ ] Interactive job selection
- [ ] Watch mode (re-run on file changes)
- [ ] Colored output and progress bars

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
