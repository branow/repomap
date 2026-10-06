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
- **A macOS build is signed, never ad-hoc.** The keychain binds a stored secret to
  the signing identity, and an ad-hoc signature anchors to the cdhash - so an
  unsigned rebuild silently loses access to every token it stored. Build with
  `scripts/sign.sh`, which refuses a requirement that still mentions a cdhash.
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

## Documentation

A behaviour change lands with the docs that describe it, in the **same commit**. Not
afterwards and not in a sweep later: this repository has already carried a flag, three
summary columns, a skill and two settings that existed only in the source, and every one
of them was found by diffing the docs against the code rather than by reading them.

Which doc owns what, so there is nothing to decide:

| You changed | Update |
|---|---|
| a flag, a command, a summary column, a column in the tree | `docs/CLI.md` - **including its example output**, which is wrong the moment a column moves |
| a decision, or the reasoning behind one - above all a repaired bug | `docs/DESIGN.md` |
| what the tool is for, or how many skills it has | `README.md` |
| a skill, a hook, a setting | `plugins/repomap/README.md` and that skill's `SKILL.md` |

Two rules of thumb. Reasoning belongs in `docs/DESIGN.md` and procedure in
`docs/CLI.md`; when a section needs both, the procedure stays and the reasoning becomes
a sentence and a link, because the same argument in two files goes stale in one of them.
And a doc that disagrees with the code is worse than no doc at all, because somebody
believes it.

`gofmt` has no opinion about prose, so what enforcement exists is in tests:
`cmd/flags_test.go` fails when a flag is undocumented, and the plugin's
`consistency.test.js` fails when a skill, hook or slash command is named and missing.
Extend those rather than trusting a reader to notice.

## Commits

The repo builds and passes tests at **every** commit. One commit is one meaningful step,
landed with the tests and the docs that prove and describe it. Never "commit now,
document later".

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
