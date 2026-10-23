---
name: repomap
description: Use when a task touches a repository - which one owns a feature, what it is built with, where something is configured, or what an account holds. Reads a local index; no clone, no search.
allowed-tools: [Read, Glob, Grep, Bash(node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js*)]
---

# repomap - Read

Reading the index is plain files: no credential, no binary, no search.

## Step 1: Locate the index

```bash
node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js list
```

Prints every tree on this machine, the workspaces in each and when they were
built. `<WS>` below is `<printed path>/<workspace>`.

Nothing listed means no index exists: say so and offer `/repomap-build`. Do not
clone repositories instead.

## Step 2: Find the repository

`<WS>/repos-active.tsv`. One row per repository: name first, description last.
It omits archived and deprecated repositories and those with no commit in a year;
`<WS>/repos.tsv` is the full list, for something old or archived.

Read the whole file rather than grepping it - it is a list of one-sentence
descriptions, which is how you find a repository whose name you do not know.
`activity` and `fingerprint` are build state; ignore them. A description in
parentheses - `(archived)`, `(deprecated)` - means the repository exists, is dead,
and was deliberately not described.

## Step 3: Read the repository

`<WS>/files/<repo>.tsv` - every tracked path, with lines, size, and a phrase on
the ones whose purpose the path does not give away. A row with no phrase is the
common case, not a gap. The header carries the repository's sentence, its branch
and its last commit.

## Step 4: Read the code

Step 1 printed a clone directory and how many repositories are in it. The file
you found in Step 3 is at `<clone dir>/<repo>/<path>` - read or grep it there.

Missing, or `0/N on disk`? Use Skill(repomap-sync) to fetch it. Do not clone by
hand: the index carries each repository's clone url and the sync uses it.

## Step 5: Cross-repository questions - grep the tree

A description names what a repository is, not everything in it.

```bash
# repositories built with a thing, or touching an area
grep -i kafka <WS>/repos.tsv

# repositories holding a file of this shape
grep -rl "Dockerfile" <WS>/files/
```

## Limits

The index is structure; the clones beside it are the content. A question about a
repository newer than the build date needs `/repomap-build`. The clones are a
cache this plugin resets, so never send somebody there to work - point them at
their own checkout. If a file does not match the above, `<printed path>/README.md`
is the tree's own format reference.
