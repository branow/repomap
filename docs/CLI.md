# repomap CLI reference

Every command takes `-o json`, every prompt has a flag, and `--no-input` turns a
missing value into an error instead of a hang.

## Flags every command takes

```
  --profile string          profile to use, overriding the current one
  -o, --output string       output format: table or json
  --no-input                never prompt; a missing value is an error
  --quiet                   suppress status messages
  --force                   proceed where a command would otherwise refuse
  --allow-plaintext         allow a 0600 file when the keychain is unavailable
```

`--force` means something specific per command: on `index` it rebuilds
everything, ignoring both staleness tiers, and on `workspace remove` it drops an
entry a profile still binds. `--allow-plaintext` is the only way a secret
reaches disk outside the keychain, and it is per invocation - the standing
setting is `secrets.fallback`.

## What a build writes

```
.repomap/
  workspaces.tsv    one row per workspace: forge, account, counts, when it was built
  README.md         the layout, for whoever reads the tree
  <workspace>/
    repos.tsv         name, files, size, commit, remote, activity, fingerprint, description
    repos-active.tsv  the same rows, minus skipped ones and those with no commit in a year
    files/<repo>.tsv  one per repository: every tracked file, some with a phrase
```

The two files at the root are what let a tree be read by someone who never
installs `repomap`: `workspaces.tsv` says what is indexed and how old it is, and
`README.md` says how to read it.

`repos.tsv` — one row per repository, with a sentence that says what it *is*:

```
repo             files  size    commit      remote                                  activity              fingerprint       description
order-service    412    18 MB   2026-09-11  git@example.internal:acme/order-service.git  2026-09-11T08:22:01Z  8a928f2e9dabdf50  Places and tracks orders, Java and Spring Boot, publishing to Kafka.
storefront       1204   64 MB   2026-09-27  git@example.internal:acme/storefront.git     2026-09-27T16:04:55Z  b8faf991d5fdbc5d  The customer-facing storefront, Next.js and React, calling order-service over REST.
legacy-importer  88     3 MB    2021-04-02  git@example.internal:acme/legacy-importer.git 2021-04-02T09:10:00Z  94141fe08d0f22c2  (archived)
```

`remote` is what makes the tree enough to reach the code it maps: a reader who
pulled a published index has no forge credential and no binary, and the plugin
clones from this column alone. Any credential the forge embedded in an https url
is stripped before it is written.

A description in parentheses is not a description: it says the repository was
listed and deliberately not described.

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

A row with no phrase is a file whose path already says what it is - the common
case, not a gap. Everything is plain text, so an agent — or `grep` — reads it
without a parser.

## Workspaces and backends

Workspaces and model backends are two independent named sets; a profile binds one
of each. That way one model backend serves every account without repeating its
API key.

```sh
repomap workspace add acme --forge github --slug acme --auth token
repomap workspace add tools --forge bitbucket --slug acme-tools \
  --auth token --username reader --exclude -archive --deprecated legacy-billing
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
`--dir`, reading them where they are and never writing to them.

A Bitbucket app password is presented as basic auth and so needs `--username`; a
GitHub token stands alone. A self-hosted forge takes `--base-url` pointing at its
API root. `--exclude` is repeatable and drops any repository whose name contains
the substring; prefixed with `=`, as in `--exclude =api`, it drops only the
repository with exactly that name, so `api-gateway` stays. An excluded repository
is not in the index at all.

`--deprecated <name>` is for a repository that is dead but should still be found
as dead. It is repeatable and matches the exact name. The repository stays in
`repos.tsv` with `(deprecated)` for its description, the way an archived one
shows `(archived)`, and is never cloned, never described and left out of
`repos-active.tsv`. On Bitbucket Cloud, which has no archive flag, it is the only
way to mark a repository archived. The list lives in the workspace entry as
`deprecated:`, so it can also be edited in the config file.

`workspace add` probes the forge before it stores anything; `--no-verify` stores
without probing. The token is prompted for, or read from stdin with
`--token-stdin` when no terminal is attached.

Clones land under the user's cache directory; `--clones <dir>` points a workspace
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
  --clones string       where working trees are kept for this run
  --backend string      model backend to describe with
  --active-years int    how recent a commit keeps a repository in the active view
  --dry-run             report the plan; clone nothing and make no model call
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
remaining     0
reasons       moved 9, new 3
describe      at most 12, decided by content fingerprint after the scan
```

## Where the working trees go

A build clones what it scans, shallow and single-branch, and keeps them: the
second run fetches instead of cloning, which is most of why it is fast.

They land in `<user cache>/repomap/clones/<workspace>/<repo>` unless the
workspace sets `clones`, or the run passes `--clones`. The Claude Code plugin
always passes its own directory, so a machine running both keeps one copy rather
than two - and the plugin, not the binary, is the authority on that path.

These are a cache the tool owns: an update is `fetch` then **`reset --hard`**, and
a directory too broken to fetch is deleted and cloned again. Never work in one.
A working tree the `local` forge reported is the opposite - never written to,
because it belongs to whoever put it there.

### What a real run reports

```
$ repomap index --limit 20
workspace      acme
out            .repomap/acme
repositories   148
scanned        20
untouched      0
skipped        2
dropped        0
remaining      126
reasons        new 20
described      18
unchanged      0
active         96
empty          new-service, spike-2025
broken         legacy-importer
missing        0
input tokens   412
output tokens  38104
cost           0.94
```

`remaining` is what `--limit` left for the next run; run it again to continue.

The last three are the ones to read carefully, because they mean different
things:

- **`empty`** holds no commits yet. There is nothing to index and nothing to
  fix - a repository created last week and not pushed to looks exactly like
  this. Note that a forge still names a default branch for one, so a clone
  attempt fails on a branch nobody created; repomap asks the remote what it
  actually has before deciding.
- **`broken`** could not be cloned or read. The tree is correct without it, and
  it is named rather than counted so somebody can go and look. Usually access.
- **`missing`** was scanned and came back from the model without a sentence.
  This one **fails the run** (exit 6): a tree whose description column is empty
  is not a success. A blank row is re-described next build rather than treated as
  untouched, so the fix is to run again.

## Checking a workspace

`repomap doctor` reports one line per check and sends almost nothing: one
listing call and one probe of one clone url, with no clone, no file read and no
model call.

```
CHECK       STATUS  DETAIL
git         pass    installed and runnable
credential  pass    found under forge:acme
forge       pass    github acme via token
listing     pass    148 repositories listed
clone       pass    git reached git@github.com:acme/order-service.git
backend     pass    haiku: claudecode claude-haiku-4-5
```

It separates the two failures that look alike from a terminal: a token the forge
refused, and a remote this machine cannot reach.

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
| 3 | Validation error — bad flags, unknown name, unsupported configuration, an account that listed nothing |
| 4 | Authentication or authorization failure |
| 5 | Not found |
| 6 | Unavailable — forge unreachable, git remote unreachable, rate limited |

A repository left without a description exits 6: a tree whose description column
is empty is not a success. One that could not be cloned does not fail the run -
it is a single missing row, and the summary names it.

Settings resolve as **flag > environment > config file > built-in default**.
Configuration lives in `~/.config/repomap/config.yml` (`%AppData%\repomap` on
Windows) and never contains a secret.

## Signing, on macOS

The keychain binds each stored secret to the code identity of the binary that
stored it, and Go ad-hoc signs by default - an identity that is the binary's
content hash, so it changes with every build. Unsigned, repomap loses access to
every token it stored the moment it is rebuilt or upgraded. Signing with a
certificate replaces that hash with the identifier plus the signing authority,
which survives both. [docs/DESIGN.md](DESIGN.md) has the reasoning.

Releases are signed by `scripts/sign.sh`, which goreleaser runs after each macOS
build and which fails if the requirement still mentions a cdhash. The release
workflow imports the certificate into a throwaway keychain from three repository
secrets:

| Secret | Holds |
|---|---|
| `MACOS_SIGN_P12` | the certificate and its key, as a base64-encoded `.p12` |
| `MACOS_SIGN_P12_PASSWORD` | the `.p12` password |
| `REPOMAP_SIGN_IDENTITY` | the certificate's common name |

`TAP_GITHUB_TOKEN`, a token that can push to the Homebrew tap and the Scoop
bucket, is the fourth secret a release needs.

The release certificate must never change: every token a user stored is bound to
it, so a new certificate locks every upgrading user out of their tokens once.
Keep the `.p12` backed up outside the repository.

Building locally needs an identity of your own, created once:

1. Keychain Access > Certificate Assistant > **Create a Certificate**
2. Name `repomap-dev`, identity type **Self Signed Root**, certificate type
   **Code Signing**
3. `export REPOMAP_SIGN_IDENTITY=repomap-dev`

Then sign after building, and the keychain stops asking:

```sh
go build -o ~/.local/bin/repomap . && scripts/sign.sh ~/.local/bin/repomap
```

A self-signed certificate is enough here, because the requirement pins the
certificate rather than the binary. Releases are self-signed for the same reason;
the Homebrew cask strips quarantine so Gatekeeper does not block them. A Developer
ID Application certificate would pass Gatekeeper on its own, but switching to one
is a certificate change, with the cost above.

`REPOMAP_SECRET_FORGE_<NAME>` bypasses the keychain entirely, which is also how
a CI runner works.

## Installing from source

```sh
go install github.com/branow/repomap@latest
```

Requires Go 1.26+ and git on `PATH`. On macOS, build with `CGO_ENABLED=1` (the
default for a native build). The keychain backend uses the Security framework so
each stored secret is bound to the binary that stored it; a `CGO_ENABLED=0` macOS
build silently falls back to a weaker path.
