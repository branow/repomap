# repomap design

Why the tool is built the way it is. For the user-facing description, see the
[README](../README.md).

## The problem

A coding agent working across an unfamiliar account spends its first minutes
rediscovering which repository owns what: list them, open five, grep for a name
that turns out to be in a sixth, read a README that was last true in 2023. That
work is repeated in full next session against an account that barely changed.

The index is therefore optimised for *reading*, not for completeness. It is a
small tree of TSV files with one line per repository and one line per file,
because an agent can read those in one pass without a parser, and a person can
`grep` them.

## Output shape

Every repository gets a catalog row with a one-sentence description, and a file
of its own listing every tracked path.

The test for what earns a file is whether one sentence can substitute for the
thing. For a repository's *purpose* it can, and the catalog row carries that. For
"which file configures the router" it cannot — a one-sentence description names
what the repository is by construction and is lossy on purpose — so only the
inventory answers it, and grepping a local tree answers it with no clone.

Every tracked path is listed, not only the annotated ones. A phrase per file is
what the model was asked for and what it charges for; the inventory is free, and
a reader looking for a specific file needs the paths that were never worth a
phrase just as much as the ones that were.

Paths come from `git ls-files`, not from walking the directory. An index that
carried build output and vendored dependencies would be mostly noise, and
`.gitignore` already says which is which.

Staleness state (`activity`, `fingerprint`) lives in the catalog itself rather
than in a sidecar file. A parallel state file drifts out of step with the index
beside it, and the columns beside them — file count, size, last commit date —
earn their place for a reader anyway.

That choice is why a build **merges** into the tree it finds rather than
replacing it. A narrowed build (`--match`, `--limit`) covers only the
repositories it selected, and rewriting the catalog from that selection alone
would delete the fingerprint of every repository the run did not look at — so
the next full build would see them all as new and buy every description again. A
row is replaced when this run covered it, removed when the forge no longer has
the repository, and otherwise left exactly as it was. Rows are written sorted by
name, so the file does not depend on which repositories a run happened to select.

An archived repository is kept in the catalog and never described. "This exists
and is frozen" is exactly the answer a reader looking for it needs, and costs a
row; spending a model call on it is what is not worth doing. Its row says
`(archived)` in the description column rather than being left blank, because a
blank is indistinguishable from a describe that failed.

### The active view

`repos-active.tsv` is the same catalog with repositories nobody has committed to
in a year left out. It is a **view**, not state: it carries no fingerprint, is
never read back by a build, and is rewritten from the catalog every run.

It exists for the reader alone, and it is the one derived file in a tree that
otherwise stores nothing derivable. Finding the right repository means reading
the catalog *whole* — that is what a list of one-sentence descriptions is for,
and grepping it only finds repositories you could already name. On an account
that has accumulated a decade of experiments, most of those rows are dead. A
reader who opens the view reads a third of the bytes and finds the same answer.

The cutoff is a year rather than a quarter because a service that is finished and
correct gets a dependency bump twice a year and is still exactly the repository
somebody is looking for. A row whose commit date is missing or unparseable is
kept: the view hides what is certainly dead, and an undated row is not certainly
anything.

### The tree describes itself

At the index root, beside the per-workspace directories, a build writes
`workspaces.tsv` — one row per workspace, with its forge, its counts and the
time it was built — and `README.md`, the layout addressed to whoever reads the
tree.

Those two files exist because **the reader is not the builder**. Building needs
the binary, a forge credential and clone access; reading needs none of them, and
the whole point of writing plain text is that anyone who can open a file can use
the result. A reader who arrives with a clone and nothing else has no way to ask
what was indexed, how old it is, or what the columns mean — so the tree answers
all three itself rather than depending on a tool, a config file, or a wiki page
that will not be there.

The roster merges for the same reason the catalog does: each workspace is built
by its own run, often days apart, so a build that rewrote the whole file would
erase the counts and the stamp of every workspace it did not touch. A dry run
writes neither file, because stamping a build that never happened is worse than
having no stamp at all.

## Staleness: two tiers

The central idea, and the thing this tool has that a single-tier index does not.
Two signals guard two different costs.

- **The forge's activity stamp decides whether to SCAN.** It is free — already in
  the repository listing — and never misses a real change, but it over-reports
  badly: any push moves it, and a repository under active development moves it
  every day.
- **A content fingerprint decides whether to DESCRIBE.** Computed locally on what
  came back. Scanning is a shallow pull; describing is a model call.

So an account where thirty repositories saw a commit this week pulls thirty and
describes the two whose shape actually changed. Without the second tier it would
be thirty calls, every run, forever — which is what an index that keys on the
commit hash does.

Not every forge has an activity signal — a directory of checkouts has none — so
the signal is optional. A repository without one always scans, and the content
fingerprint does all the real gating. The design degrades to one tier without
special-casing a forge by name.

### Fingerprints

The fingerprint is taken over exactly what the describer sees: the inventory of
paths, the README, the default branch, and the forge's own summary. A fact the
model reads but the hash does not is a fact that can change the answer without
ever earning a fresh one. The canonical form is built as explicit lines rather
than serialised, so adding an unrelated field to an internal struct cannot move
the hash.

**The commit hash is deliberately absent.** HEAD moves on every commit, including
the ones that fix a typo inside a file the index names but does not read. Hashing
it is the single-tier behaviour, spelled differently.

Line counts travel with paths, bucketed by powers of two. A file going from 40
lines to 41 is the same file; one going from 40 to 900 is not the thing the last
description described. Growth that matters here is multiplicative: the gap
between 10 and 20 lines means something, the gap between 1010 and 1020 does not.

## Describing

One call per repository, never a batch.

`dbmap`, the same design pointed at a database, batches several objects into one
call, and it is right to: a two-column lookup table's prompt is a few hundred
characters, so a call per object would be a thousand calls buying a thousand
sentences. A repository's prompt is its whole file list. Two of them in one call
is a prompt the model answers worse, for a saving of one request — and a failure
then loses both.

Calls run concurrently instead, several at a time, and are merged back in a fixed
order so the result does not depend on which finished first. Describing is the
long pole of a build and the repositories are independent; the ceiling that
matters is the provider's rate limit, not this machine.

Answers are filed under the repository that was **asked** about, never under
whatever the model echoed back. A phrase whose path matches no file is dropped
rather than a row being invented for it, because a phrase attached to no file is
a row the reader cannot check.

A failure is per repository. One repository the provider choked on must not
discard the two hundred sentences a run has already paid for — but a repository
left without a sentence is reported and fails the run's exit status, because a
tree whose description column is empty is not a success however few calls errored.

A failed describe still writes its row, with its fingerprint. Without that, the
activity signal would match next run, the row would count as untouched, and the
column would stay empty forever. The `undescribed` scan rule exists to pick those
rows up.

### Two prompt lessons

Both are inherited from `dbmap`, both cost real debugging there, and both are
pinned by tests here.

**Never name the output field `description`.** With a field named `description`
whose schema text read "one sentence saying what this repository does", every
model tested filled it with a description *of the field*:

```
One-sentence summary of the repository's purpose and technology stack.
```

Better instruction-following made it more reliably wrong. Naming the field
`sentence`, with schema text phrased as an order — "Put the finished sentence
here verbatim. Never describe what the sentence would say" — fixes every case.
The per-file field is `purpose` for the same reason.

**Give a verb-first instruction with worked examples.** Listing requirements
("say what it does and what it is built with") invites a model to restate the
list. Naming the failure mode and showing a good and a bad answer fixes it:
"the customer-facing storefront, Next.js and React, talking to the order service
over REST" is useful; "a web application repository containing source code" is
not.

## What never reaches a model

Paths, the root README cut at 3,000 characters, the forge's own one-line summary,
and a two-level directory sketch. **No file contents.**

That is a hard line rather than a budget. A stage that started reading source to
improve a description would be a stage that sends a private codebase to a third
party, and it is not what the sentence is for: a reader wants to know which
repository and which file, then goes and reads the code.

The directory sketch exists because a flat list of two thousand paths is a poor
way to tell a model what a repository *is*, while the shape of its top two levels
is a very good one — and because that sketch is what a human reads first too.

## Clones

Shallow, single-branch, no tags. The index reads the tip and nothing else, so a
full history is minutes and gigabytes bought for a question nobody asks of this
tool.

A clone this tool made is a cache it owns: a local change in one is damage to
repair, not work to preserve, so an update resets rather than merges, and a
half-written directory from a killed run is removed and recloned. A working tree
the forge reported as already on disk is the opposite — it is never written to,
because it belongs to whoever put it there, which is what makes it safe to point
this at a checkout tree something else maintains.

Clones land under the user's cache directory rather than in the index tree or the
home directory: they are reconstructible from the forge, and anything that clears
a cache is welcome to clear them. Removing a workspace does not delete them,
because a command that silently deleted gigabytes of checkouts on a config edit
is a command nobody runs twice.

Git is shelled out to rather than linked. The machine running a build already has
git configured with the credential helper, the proxy and the SSH keys that reach
the forge, and reimplementing that is how a tool ends up with its own broken half
of an authentication story. Every invocation runs with `GIT_TERMINAL_PROMPT=0`: a
build runs unattended, and a git that asks for a passphrase hangs it forever.

Access is probed **once**, before anything is cloned. The failure that matters —
no network, no VPN, an expired key — fails every repository identically, and
probing each would turn one message into two hundred.

## Structure

Dependencies flow bottom-up; siblings do not import each other, and the domain
never imports the shell.

```
main.go
└── cmd/                     cobra tree; flag parsing only
    └── internal/cmdutil     Factory: the dependency bundle, and ExitCode
        ├── internal/config        named workspaces, backends, profiles
        ├── internal/credentials   keychain store, env path, opt-in plaintext
        ├── internal/iostreams     streams, tty, prompts
        ├── internal/output        table and json writers
        │
        ├── internal/index         the build pipeline
        │   ├── internal/forge         Forge interface: where a listing comes from
        │   │   ├── internal/forge/bitbucket
        │   │   ├── internal/forge/github
        │   │   └── internal/forge/local
        │   ├── internal/clone        working trees: clone, update, probe
        │   ├── internal/scan         a working tree -> the file inventory
        │   ├── internal/plan         the two-tier staleness planner
        │   │   └── internal/fingerprint
        │   ├── internal/describe     prompt, concurrency, path matching
        │   │   └── llm (module)          provider seam
        │   └── internal/render       the TSV tree
        │
        ├── internal/git         the git command line, a leaf
        └── internal/catalog     the shared vocabulary; imports nothing
```

### Adding a forge

Everything above `internal/forge` is forge-agnostic. A new forge implements one
method:

```go
type Forge interface {
    Name() string
    Repositories(ctx context.Context) ([]catalog.Repo, error)
}
```

and registers itself from its own package init, so adding a forge is adding a
package and importing it in `cmd/root.go` — never editing a switch.

It lists and nothing else. Obtaining a working tree is `internal/clone`'s job,
because a repository read where it already sits on disk needs no forge at all,
and the two REST forges differ only in the shape of a listing.

The pagination loop, the credential presentation and the status mapping are
shared. A forge supplies only how to decode one page — Bitbucket puts the next
page in the body, GitHub in a `Link` header — so neither carries a request loop
of its own, and neither invents a third meaning for HTTP 403.

Two per-forge facts are **data**, not branches: which auth modes it accepts, and
whether it is remote at all. The local forge is not a special case in the
pipeline; it is a row in a table that says it reads a directory and takes no
credential.

## Safety and privacy

- **Tokens never enter the config file.** They live in the OS keychain, or come
  from the environment. There is no silent fallback to a plaintext file: a
  keychain failure is an error naming the fix, because a transient failure would
  otherwise persist a credential to disk permanently.
- **A public account and a directory of checkouts need no credential at all**,
  and are not asked for one. That is what makes the tool runnable by somebody who
  has not been given access to anything yet.
- **SSH is preferred over HTTPS** for a clone url, because that is the transport
  a developer's git is already authenticated for; the HTTPS link a forge returns
  carries no credential and would prompt.
- **A listing asks for only the columns it uses.** Bitbucket returns a large
  object per repository by default and honours a field selector, which on a large
  workspace is the difference between a few hundred kilobytes and a few megabytes.
- **A response body is capped.** A response far past a listing page's size is a
  proxy's error page or a login form, and reading it whole into memory to then
  fail to parse it helps nobody.

## Testing

No test touches a real forge, a real git, a real keychain, a real model or the
network. Every external dependency sits behind an interface with an in-memory
fake, and the rules above are pinned by tests that fail loudly if someone weakens
them: that the commit hash stays out of the fingerprint, that a second run over
unchanged input makes no model call, that a narrowed run does not make the next
full one pay, that a failed describe leaves a row the next run repairs, that an
unreadable repository is skipped rather than fatal, that the source is closed
before the first model call.

The real-keychain test is gated behind both a build tag and an environment
variable, so `go test ./...` never touches the developer's login keychain.
