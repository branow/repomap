# repomap CLI reference

Every command takes `-o json`, every prompt has a flag, and `--no-input` turns a
missing value into an error instead of a hang.

## What a build writes

```
.repomap/
  workspaces.tsv    one row per workspace: forge, account, counts, when it was built
  README.md         the layout, for whoever reads the tree
  <workspace>/
    repos.tsv         name, files, size, commit, activity, fingerprint, description
    repos-active.tsv  the same rows, minus repositories with no commit in a year
    files/<repo>.tsv  one per repository: every tracked file, some with a phrase
```

The two files at the root are what let a tree be read by someone who never
installs `repomap`: `workspaces.tsv` says what is indexed and how old it is, and
`README.md` says how to read it.

`repos.tsv` — one row per repository, with a sentence that says what it *is*:

```
repo             files  size    commit      activity              fingerprint       description
order-service    412    18 MB   2026-09-11  2026-09-11T08:22:01Z  8a928f2e9dabdf50  Places and tracks orders, Java and Spring Boot, publishing to Kafka.
storefront       1204   64 MB   2026-09-27  2026-09-27T16:04:55Z  b8faf991d5fdbc5d  The customer-facing storefront, Next.js and React, calling order-service over REST.
legacy-importer  88     3 MB    2021-04-02  2021-04-02T09:10:00Z  94141fe08d0f22c2  (archived)
```

`activity` and `fingerprint` are build state; ignore them. A description in
parentheses is not a description — it says the repository was listed and
deliberately not described.

`files/order-service.tsv` — every tracked path, with a phrase on the ones whose
purpose the path does not already give away:

```
# order-service @8a928f2e9dabdf50
# 412 files  18 MB  last commit 2026-09-11  branch main
# Places and tracks orders, Java and Spring Boot, publishing to Kafka.
path                                          lines  size    purpose
src/main/java/.../OrderController.java        240    9 KB    REST entry points for orders
src/main/java/.../OrderStateMachine.java      612    24 KB   allowed order status transitions
src/main/resources/application.yml            61     2 KB
pom.xml                                       140    5 KB
```

A row with no phrase is a file whose path already says what it is. That is the
common case, and not a gap.

Everything is plain text, so an agent — or `grep` — reads it without a parser.

## Workspaces and backends

Workspaces and model backends are two independent named sets; a profile binds one
of each. That way one model backend serves every account without repeating its
API key.

```sh
repomap workspace add acme --forge github --slug acme --auth token
repomap workspace add tools --forge bitbucket --slug acme-tools \
  --auth token --username reader --exclude -archive
repomap workspace add local --forge local --dir ~/src --auth none
repomap workspace list
repomap workspace show acme
repomap workspace remove acme

repomap backend add gpt --provider openai --model gpt-4o-mini
repomap backend add local-llm --provider openai --base-url http://127.0.0.1:11434/v1

repomap profile create dev --workspace acme --backend gpt
repomap profile switch dev
```

Supported forges are `bitbucket`, `github` and `local`. Authentication is `token`
or `none`.

`local` needs no credential and no network: it indexes the checkouts already in
`--dir`, reading them where they are and never writing to them. It is the fastest
way to see what this tool produces, and the right choice when something else
already maintains a checkout tree.

A Bitbucket app password is presented as basic auth and so needs `--username`; a
GitHub token stands alone. A self-hosted forge takes `--base-url` pointing at its
API root. `--exclude` is repeatable and drops any repository whose name contains
the substring.

Clones land under the user's cache directory. `--clones <dir>` points a workspace
at a tree something else maintains instead.

Backends are `anthropic`, `openai` (any OpenAI-compatible endpoint, including
Ollama and vLLM) and `claudecode`, which shells out to the Claude Code CLI and
needs no API key of its own.

## Building an index

```sh
repomap index [workspace] [flags]

  --match string        only repositories whose name contains this substring
  --limit int           scan at most this many repositories; 0 means all
  --out string          where the index tree is written (default ".repomap")
  --backend string      model backend to describe with
  --active-years int    how recent a commit keeps a repository in the active view
  --dry-run             report the plan; clone nothing and make no model call
  --quiet               silence progress
```

`--match` and `--limit` cover only what they selected and **merge** into whatever
the tree already holds: rows they did not touch keep their descriptions and their
fingerprints, so a narrowed run never makes the next full one pay to redescribe
the rest. That is what makes them the right way to try `repomap` against an
account you do not own.

The persistent `--force` flag rebuilds everything, ignoring both staleness tiers.

### Progress output

Progress goes to **stderr** and the summary to stdout, so `-o json` pipes cleanly
while a person watching the terminal sees the work. Every stage that does work
per repository names that repository *before* it is read, so a run that stalls
says what it stalled on:

```
manifest: 148 repositories
plan: 12 repositories to scan, 134 repositories the activity signal proves untouched, 2 repositories skipped
scan: 1/12 order-service
scan: 2/12 storefront
fingerprint: 3 repositories changed, 9 repositories unchanged
describe: order-service  Places and tracks orders, Java and Spring Boot, publishing to Kafka.
describe: storefront  The customer-facing storefront, Next.js and React, calling order-service over REST.
write: 148 rows in repos.tsv, 96 rows in repos-active.tsv
```

### What a dry run can and cannot say

`--dry-run` reports only what it can actually know. It reads the listing and
stops, so it can say how many repositories must be pulled and why — but not how
many will be described, because that is decided by a fingerprint of content it
has not read. It reports an upper bound and says so, rather than printing a guess
in the column a real run fills with a fact:

```
$ repomap index --dry-run
workspace     acme
out           .repomap/acme
repositories  148
dry-run       true
scan          12
untouched     134
skipped       2
dropped       0
reasons       moved 9, new 3
describe      at most 12, decided by content fingerprint after the scan
```

## Checking a workspace

`repomap doctor` reports one line per check and sends almost nothing — one
listing call and one probe of one clone url. Nothing is cloned, no file is read
and no model is called:

```
CHECK       STATUS  DETAIL
git         pass    installed and runnable
credential  pass    found under forge:acme
forge       pass    github acme via token
listing     pass    148 repositories listed
clone       pass    git reached git@github.com:acme/order-service.git
backend     pass    haiku: claudecode claude-haiku-4-5
```

The two failures worth separating are the ones that look alike from a terminal: a
token the forge refused, which no amount of waiting fixes, and a remote this
machine cannot reach, which is usually a VPN that dropped.

## Scripting and CI

Secrets can come from the environment (`REPOMAP_SECRET_FORGE_<NAME>`,
`REPOMAP_SECRET_LLM_<NAME>`), which is how a CI runner works with no keychain at
all.

Exit codes are meaningful:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic failure |
| 2 | Cancelled at a prompt |
| 3 | Validation error — bad flags, unknown name, unsupported configuration |
| 4 | Authentication or authorization failure |
| 5 | Not found |
| 6 | Unavailable — forge unreachable, git remote unreachable, rate limited |

A run where some repository was left without a description exits 6, because a
tree whose description column is empty is not a success. A repository that could
not be cloned does not fail the run: it is one row missing from a tree that is
otherwise correct, and the summary names it.

Settings resolve as **flag > environment > config file > built-in default**.
Configuration lives in `~/.config/repomap/config.yml` (`%AppData%\repomap` on
Windows) and never contains a secret.

## Installing from source

```sh
go install github.com/branow/repomap@latest
```

Requires Go 1.26+ and git on `PATH`. On macOS, build with `CGO_ENABLED=1` (the
default for a native build). The keychain backend uses the Security framework so
each stored secret is bound to the binary that stored it; a `CGO_ENABLED=0` macOS
build silently falls back to a weaker path.
