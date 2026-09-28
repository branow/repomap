---
name: repomap
description: Use when a task touches a repository - which one owns a feature, what it is built with, where something is configured, or what an account holds. Reads a local index; no clone, no search.
allowed-tools: [Read, Glob, Grep, Bash(node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js*)]
---

# repomap - Read

Four steps against plain files. No clone, no credential, no binary.

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
It omits repositories with no commit in a year; `<WS>/repos.tsv` is the full
list, for something old or archived.

Read the whole file rather than grepping it - it is a list of one-sentence
descriptions, which is how you find a repository whose name you do not know.
`activity` and `fingerprint` are build state; ignore them. A description in
parentheses - `(archived)` - means the repository was listed and not described.

## Step 3: Read the repository

`<WS>/files/<repo>.tsv` - every tracked path, with lines, size, and a phrase on
the ones whose purpose the path does not give away. A row with no phrase is the
common case, not a gap. The header carries the repository's sentence, its branch
and its last commit.

## Step 4: Cross-repository questions - grep the tree

A description names what a repository is, not everything in it.

```bash
# repositories built with a thing, or touching an area
grep -i kafka <WS>/repos.tsv

# repositories holding a file of this shape
grep -rl "Dockerfile" <WS>/files/
```

## Limits

Structure, not content. A question about what code does, or about a repository
newer than the build date, needs the clone - `roots.js list` prints where it is.
If a file does not match the above, `<printed path>/README.md` is the tree's own
format reference.
