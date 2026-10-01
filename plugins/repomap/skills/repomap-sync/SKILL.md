---
name: repomap-sync
description: Use when a repository's code needs to be on this machine to read - the index named a file and the clone is missing or out of date, or a developer asks to sync a repo. Fetches from the index's own clone urls; no forge credential.
argument-hint: [<repo>] [--workspace=N]
allowed-tools: [Bash(node ${CLAUDE_PLUGIN_ROOT}/scripts/sync.js*), Bash(node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js*)]
---

# repomap - Sync

The index says which repository and which file; a clone is what lets you open
it. This fetches them.

A session-start hook already does this in the background, on anything older than
12 hours, so most of the time the clone you want is there. Use this when it is
not, or when you need the very latest.

## What is on disk

```bash
node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js clones
```

Prints each workspace, how many of its repositories are cloned, and where.

## Fetch

```bash
node ${CLAUDE_PLUGIN_ROOT}/scripts/sync.js --repo=<name>
```

One repository, now, ignoring the staleness window. Without `--repo` it fetches
everything the index lists that is older than the window; add
`--threshold=0` to force all of them and `--prune` to delete the clones of
repositories the index no longer lists.

Clone urls come from the index itself, so this needs no forge credential and no
`repomap` binary. What it does need is git access to the forge: a failure here is
the network, the VPN, or an SSH key, and the message says so.

## Limits

These clones are a **cache this plugin owns**. A fetch is followed by a hard
reset, so never edit one and never send a developer to one to work in - point
them at their own checkout. A repository the index lists with no clone url was
indexed by an older `repomap`; `/repomap-build` fixes it.
