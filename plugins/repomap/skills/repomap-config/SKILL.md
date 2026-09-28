---
name: repomap-config
description: Use when a developer wants to see which repository indexes this machine reads, connect it to one a team publishes, fetch or update that index, or be reminded (or stop being reminded) when an index is overdue a rebuild.
argument-hint: [list | add <path> | add --remote=<url> [--subdir=<dir>] | sync [<name>] | remove <name> | remind <hours>|off]
allowed-tools: [Bash(node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js*)]
---

# repomap - Config

Pass `$ARGUMENTS` through and relay what it prints:

```bash
node ${CLAUDE_PLUGIN_ROOT}/scripts/roots.js $ARGUMENTS
```

- `list` - every index this machine reads, its workspaces, when each was built
- `add <path>` - read a tree built elsewhere on this machine
- `add --remote=<git url> [--subdir=<dir in the repo>] [--branch=<branch>]` -
  read a tree a team publishes; follow with `sync`
- `sync [<name>]` - clone or update the published trees
- `remove <name>`
- `remind <hours>` / `remind off` - session-start warning that an index is
  overdue, off until set

An index committed in the current repository is found automatically and is never
registered. Exits non-zero on a bad argument.

Repositories appear and are retired over weeks, so `remind 168` (a week) suits
an account better than a threshold in days. When an index is overdue, offer
`/repomap-build` rather than repeating the warning.
