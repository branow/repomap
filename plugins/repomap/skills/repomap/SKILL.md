---
name: repomap
description: Use when a task needs the right repository or the right file in one - which repository owns a feature, what a repository is built with, where something is configured, or what an account even holds. Reads a local index; no cloning, no search.
allowed-tools: [Read, Glob, Grep, Bash(node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js*)]
---

# repomap - Read

Three steps against plain files. No clone, no credential, no binary.

## Step 1: Locate the index

```bash
node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js list
```

Prints every tree on this machine, the workspaces in each and when they were
built. `<WS>` below is `<printed path>/<workspace>`.

Nothing listed means no index exists: say so and offer `/repomap-build`. Do not
clone a hundred repositories instead.

## Step 2: Find the repository

Read `<WS>/repos-active.tsv` **whole**.

It is a list of one-sentence descriptions, which is how you find a repository
whose name you do not know - grepping it only finds the ones you could already
name. It leaves out repositories with no commit in a year; `<WS>/repos.tsv` is
the full list, for something old or archived.

One row per repository: name first, description last. `activity` and
`fingerprint` are build state; ignore them. A description in parentheses -
`(archived)` - means the repository was listed and deliberately not described.

## Step 3: Find the file

Read `<WS>/files/<repo>.tsv` whole.

Every tracked path, with `lines`, `size`, and a short phrase on the ones whose
purpose the path does not already give away. A row with no phrase is a file whose
path says what it is - that is the common case, not a gap. The header carries the
repository's sentence and its last commit date.

## Cross-repository questions

The catalog is small enough to read whole, so ask it directly rather than
grepping:

```bash
# everything built with a given thing, or touching a given area
grep -i kafka <WS>/repos.tsv

# which repositories have a file of this shape
grep -rl "Dockerfile" <WS>/files/
```

## Limits

Structure, not content. This says which repository and which file; it does not
say what a function does. Go and read the code once you know where it is - the
clone is at the path `roots.js list` prints for the workspace, or clone it
yourself.

It is only as current as the `built` stamp in the roster. A repository created
since then is not here; say so rather than concluding it does not exist. If a
file does not match the above, `<printed path>/README.md` is the tree's own
format reference.
