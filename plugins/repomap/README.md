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

## Staleness

Off until asked for. `/repomap-config remind 168` reports at session start when
the stalest workspace in a tree is older than that; `/repomap-config remind off`
stops. The age comes from the build stamp the CLI writes into the roster, so a
rebuild silences it immediately.

## Tests

```sh
npm test
```

The registry, the roster parser, git against a real local remote, the staleness
decision, and the wiring that is done by string rather than by import.
