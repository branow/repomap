# Working on repomap

Instructions for a coding agent changing this repository. `README.md` says what the
tool does; `docs/DESIGN.md` says why it is built this way, including the layer tree
and the reasoning behind every constant. Read both before writing code, and do not
re-derive a constant that already has a stated justification.

## Stack

Go, one module: `github.com/branow/repomap`. Cobra for the CLI. The one model call
per repository goes through `github.com/branow/gollm`, a separate module.

Dependencies are **pinned**. Never run a bare `go get`, never upgrade a pin as a side
effect. `go mod tidy` is fine once your code has imports. If you genuinely need a new
dependency, stop and ask rather than adding one.

```sh
go build ./... && go vet ./... && go test ./...
gofmt -l .        # must print nothing
```

`gollm` is a released dependency like any other: pinned in `go.mod`, resolved from
the module proxy. There is no workspace file, and a clean clone builds with nothing
checked out beside it.

## Non-negotiables

- **No deployment-specific identifiers.** No real hostnames, account slugs, repository
  names, employer or product references - not in code, docs, tests, fixtures, or the
  prompt strings the tool sends to a model. Use `example.internal`, `acme`,
  `order-service`. Every real value is user data that lives in the user's config or
  keychain at run time, never in the tree.
- **Secrets never enter the config file**, a log line, an error message, or a test
  fixture. A workspace record holds structured non-secret fields; the token is fetched
  at request time. Do not write a credential-shaped literal into a test - build the
  value instead.
- **Nothing but paths, a README and the forge's own summary reaches a model.** No file
  contents. A stage that started reading source files to improve a description would be
  a stage that sends a private codebase to a third party, and it is not what the
  sentence is for.
- **Every clone is shallow and single-branch**, and a clone this tool made is a cache it
  may reset. A working tree the forge reported as already on disk is read and never
  written: it belongs to whoever put it there.
- **Git never prompts.** Every invocation runs with `GIT_TERMINAL_PROMPT=0`; a build
  runs unattended, and a git that asks for a passphrase hangs it forever.
- **No emoji.** Not in code, comments, docs, or commit messages.

## Code

- Errors are structured values, never formatted strings. Put context on a field, not
  into a message. There is exactly one place that turns an error into a process exit
  status: `cmdutil.ExitCode`, called from `main.go`.
- Packages in this repo trust each other. No defensive re-validation between internal
  packages - validate external input once, at the boundary, then trust it.
- Rule lists are **data walked by an engine**, never an if/else ladder. `ScanReasons`,
  `Skips`, `preflights`, `forges`, `providers` and the exit `rules` are all tables. A
  new case should be a new row.
- A forge registers itself from its own package init, so adding one is adding a package
  and importing it in `cmd/root.go` - never editing a switch.
- Dependencies flow bottom-up. Siblings do not import each other, and the domain never
  imports the shell. `internal/catalog` is the shared vocabulary and imports nothing.
- Doc-comment every exported symbol: one line on what it is, plus *why* where the code
  would otherwise look arbitrary. Comments explain why, never restate the code. No
  commented-out code, no committed TODOs, no multi-paragraph essays.
- `gofmt` clean, imports ordered, lines at most 100 columns. Prefer short,
  descriptive-within-context names.

## Tests

- Tests live beside the code as `*_test.go`. Whoever writes the code writes its tests.
- Table-driven. **No network, no real git, no real keychain, no real model call** in any
  test. Every external dependency sits behind an interface with an in-memory fake. The
  one test that touches a real keychain is gated behind both a build tag and an
  environment variable so it never runs by accident.
- Test the decision, not the implementation. Several tests exist specifically to stop a
  past mistake being reintroduced - the commit hash entering the fingerprint, the
  structured-output field being renamed to `description`, a narrowed run erasing the
  fingerprints it did not touch, a failed describe leaving a row that can never be
  repaired. If one of those fails, the fix is almost certainly in the code, not the test.

## Commits

The repo builds and passes tests at **every** commit. One commit is one meaningful step,
landed with the tests that prove it. Never "commit now, test later".

Conventional Commits: `feat:`, `fix:`, `test:`, `refactor:`, `docs:`, `chore:`. Lowercase
first word, no body, no attribution or co-author line.

## Scope

If a change you need falls outside what you were asked to do - a dependency, a `go.mod`
edit, a change to the `gollm` module, a rename that ripples - stop and report it rather
than making it. When several agents work at once, each edits only its assigned paths and
reports anything it needs elsewhere.

## Never commit

Any `config.yml`, any `credentials.yml`, any `*.local.*`, the `.repomap/`
output tree, a clone this tool made, build output, or a file containing a real
credential. `.gitignore` is allowlist-style (deny-by-default) - do not add a rule that
tracks these.
