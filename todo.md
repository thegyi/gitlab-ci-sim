# gitlab-ci-sim improvement todo

## Test coverage

- [x] Parser tests
  - [x] Variables in `image:`
  - [x] `default:`, `variables:`, `workflow:` sections
  - [x] Invalid `image:`, missing `script:`, unknown `stage:`
  - [x] `!reference` and `extends` edge cases
- [x] Resolver tests
  - [x] Nested `extends`
  - [x] `!reference` to a scalar value
  - [x] `include:` with `stages` and `variables` merging
  - [x] Missing include file error
- [x] Rules tests
  - [x] `if:` with `=~` and `!~` regex
  - [x] `when: never` / `manual` / `delayed`
  - [x] `only:`/`except:` on `refs` and `merge_requests`
  - [x] `changes:` and `exists:` matching
- [x] Variables tests
  - [x] `With()`, `Expand()`, `MissingValues()`
  - [x] CLI `-v` overrides, config variables, job variables precedence
  - [x] Git-derived values in a real temp repo
- [x] Executor tests
  - [x] Mock the `docker` CLI or use a runtime interface
  - [x] Test `MissingValues` fail-fast path
  - [x] Test `allow_failure` and non-zero exit code
- [x] Integration tests
  - [x] Run a real `alpine` container in a temp project
  - [x] Verify `before_script`, `script`, `after_script` order
  - [x] Verify a job with an undefined variable is rejected

## New features

- [x] `artifacts:` passing between jobs
- [x] `cache:` restore/save
- [x] `needs:` DAG execution
- [x] `services:` linked helper containers
- [ ] `include:` remote / project
- [x] Colored streaming output
- [ ] Pipeline graph (`gitlab-ci-sim graph`)
- [ ] Watch mode (`gitlab-ci-sim run --watch`)
- [ ] Masked / declared-only variables
- [ ] Container runtime interface (Docker / Podman / fake)
