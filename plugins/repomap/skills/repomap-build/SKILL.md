---
name: repomap-build
description: Use when a developer asks to build, update or rebuild a repository index. Runs the repomap CLI, which lists an account, pulls what moved, describes each repository with a model, and writes the index tree.
argument-hint: [<workspace>] [--match X] [--limit N]
allowed-tools: [Read, Bash(command -v repomap), Bash(repomap *), Bash(node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js*), Bash(node ${CLAUDE_PLUGIN_ROOT}/scripts/publish.js*), Bash(awk *), Bash(tail *), Monitor, TaskStop]
---

# repomap - Build

Developer tool, and the only part of this plugin that needs the `repomap`
binary. The binary is the whole build: it decides what is stale, pulls what
moved, describes repositories with a model, writes the tree. This skill points
it somewhere and reports.

A build clones repositories and costs one model call per changed repository.

## Step 0: Prerequisites

```bash
command -v repomap
repomap doctor [<workspace>]
```

Missing binary - stop, give the install line, let the developer run it:
`brew install branow/tap/repomap`, or
`scoop bucket add branow https://github.com/branow/scoop-bucket && scoop install repomap`,
or `curl -fsSL https://raw.githubusercontent.com/branow/repomap/main/scripts/install.sh | sh`.

`doctor` names a missing workspace, backend or profile; `repomap workspace add
--help` covers each. Never put a token on a command line - the CLI prompts and
stores it in the OS keychain. Everything timing out is a VPN or an expired SSH
key, not configuration.

With nothing configured, the local forge needs no token and no network:
`repomap workspace add local --forge local --dir <checkouts> --auth none`.

## Step 1: Decide `<out>`

Ask if the request does not say:

- **committed with the project** - `.repomap` (the default). Anyone who clones
  reads it; nothing to register
- **this machine only** - `node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js add`
  prints the path and registers it
- **published for a team** - a checkout of the repository that carries the tree.
  Step 6 commits and pushes it. Teammates run
  `/repomap-config add --remote=<url> --subdir=<dir>` once, then
  `/repomap-config sync`

## Step 2: Plan

```bash
node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js clones
repomap index [<workspace>] --out <out> --clones <clone dir> --dry-run
```

`roots.js clones` prints the directory this plugin keeps clones in; pass it as
`--clones` so the build fetches into the same place the sync maintains. Without
it the binary uses its own cache and the machine ends up with two shallow copies
of every repository.

Reads the account listing and stops. Relay the counts in one line. Get a yes
before Step 3 when `scan` is more than a handful - a first build of two hundred
repositories is two hundred clones and two hundred model calls.

## Step 3: Build, one workspace at a time

```bash
repomap index [<workspace>] --out <out> --clones <clone dir> 2> <log>
```

`run_in_background: true`; a first build takes minutes. Never two at once - they
share one backend and one network. Arm a `Monitor` on `tail -f -n 0 <log>`,
summarize new lines in one short line, `TaskStop` it when the run ends.

Exit codes: `4` auth, `5` not found, `6` unavailable - the forge, a git remote,
or a describe that left rows empty. Read the summary before deciding.

## Step 4: Report the two failures separately

`broken` names repositories that could not be cloned or scanned. The tree is
correct without them, so name them and do not re-run: it is access, or a
repository with no commits.

`missing` counts repositories the model left without a sentence, and fails the
run. A blank row is re-described next build rather than called untouched, so
re-run rather than hunting for them.

```bash
awk -F'\t' 'NR>1 && $NF=="" {print $1}' <out>/<workspace>/repos.tsv
```

## Step 5: Publish, if the tree is a published one

```bash
node ${CLAUDE_PLUGIN_ROOT}/scripts/publish.js --tree=<out> [--pr]
```

Commits the tree to a dated branch, pushes it, and with `--pr` opens a pull
request through `gh` or `bkt`. Skip this for a tree committed with the project -
that one belongs in the developer's own commit - and for a machine-only tree,
which has nothing to publish to.

An unchanged tree reports that and does nothing. A commit that could not be
pushed published **nothing**: report it as a failed publish rather than a
partial one, and name the branch it is stranded on.

## Step 6: Report

`node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js list` shows the tree as the
`repomap` skill will see it. Report per workspace what was described and what it
cost, plus whatever `--limit` left for the next run.

`--match` and `--limit` merge into the tree already there: untouched rows keep
their descriptions and their fingerprints, so a narrowed run never makes the next
full one pay again. That is what makes `--limit 30` three times cost what one
uncapped run would.
