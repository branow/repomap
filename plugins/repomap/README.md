# repomap

Answers questions about a set of repositories without searching them - which
repository owns a feature, what it is built with, which file to open, what an
account even holds.

It works from an index: a directory of plain TSV files, one row per repository
with a one-sentence description, and one file per repository listing every
tracked path. Reading it needs no clone, no credential and no binary. Building it
needs the [repomap](https://github.com/branow/repomap) CLI, and only on the
machine that builds.

```
/plugin marketplace add branow/repomap
/plugin install repomap@repomap
```

## Skills

| Skill | For |
|---|---|
| `repomap` | Reading the index: which repository, which file, what an account holds |
| `repomap-sync` | Fetching the repositories an index points at, so the code is there to read |
| `repomap-build` | Building or refreshing an index with the repomap CLI |
| `repomap-config` | Which indexes this machine reads, fetching a published one, the staleness reminder |

## How a tree is found

An index tree is a directory holding `workspaces.tsv` - a roster of the
workspaces under it - and one directory per workspace. Three ways one reaches a
reader, and the read skill treats them identically:

- **Committed in the repository.** `.repomap/` beside the project, found by
  walking up from wherever the session is running. Nothing is registered and
  nothing is configured; cloning the repository is the whole install.
- **Built for one machine.** Under the plugin's data directory, registered by
  the build that wrote it.
- **Published by a team.** A git repository somebody else builds into, cloned
  once and pulled since.

The last two are rows in `roots.json` in the plugin's data directory, which is
the only state this plugin owns. Uninstalling the plugin is uninstalling
everything it did.

## Clones

The index says which repository and which file; a clone is what lets you open it.
A SessionStart hook fetches, in the background, anything older than 12 hours,
reading each repository's clone url out of the index itself - so it needs no forge
credential and no `repomap` binary. `/repomap-sync` fetches one now. A repository
the build skipped - `(archived)` in its description - is never cloned, and a clone
made before it was skipped is removed.

They live under the plugin's data directory, and the build is told to use the same
place, so a machine keeps one shallow copy of each repository rather than two.
They are a cache this plugin resets: never work in one.

A failure there is silent by construction - nobody is watching a background hook -
so a `UserPromptSubmit` hook reports what went wrong at the next prompt. Problems
only, by default; `REPOMAP_NOTIFY_LEVEL=info` turns progress on for a session.

## Settings

Two, both per machine and both off or quiet by default, because nobody should be
nudged about a tool they only read from.

- `/repomap-config remind 168` reports at session start when the stalest
  workspace in a tree is older than that; `remind off` stops. The age comes from
  the build stamp the CLI writes into the roster, so a rebuild silences it
  immediately.
- `/repomap-config notify info` lowers what the activity hook reports. The
  default is `warn`, so a healthy background sync says nothing;
  `REPOMAP_NOTIFY_LEVEL` overrides it for one session.

`/repomap-config clones` says where the repositories are kept and how many are
there.

## Tests

```sh
npm test
```

The registry, the roster parser, the clone sync and publish against real local
remotes, the alerting cursor, the staleness decision, and the wiring that is done
by string rather than by import.
