<h1 align="center">repomap</h1>

<p align="center"><b>Give your coding agent a map of the codebase.</b></p>

<p align="center">
  Index every repository an account holds into plain text, then ask your agent
  which one owns a thing and which file in it to open — with no search, no
  cloning a hundred repositories to grep them, and no tokens spent
  rediscovering what it learned yesterday.
</p>

<p align="center">
  <a href="https://github.com/branow/repomap/actions/workflows/ci.yml"><img alt="ci" src="https://github.com/branow/repomap/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/branow/repomap/releases/latest"><img alt="release" src="https://img.shields.io/github/v/release/branow/repomap"></a>
  <a href="LICENSE"><img alt="license" src="https://img.shields.io/github/license/branow/repomap"></a>
  <img alt="go" src="https://img.shields.io/github/go-mod/go-version/branow/repomap">
</p>

---

Every session that starts with "where does that live?" starts from zero. List
the repositories. Open five of them. Grep for a name that turns out to be in a
sixth. Read a README that was last true in 2023. Then the context window fills,
the session ends, and the next one does the same walk again.

`repomap` does it once and writes the answers down.

## Features

- **Ask in plain language.** Which repository owns checkout, what is that
  service built with, which file configures the router, what is even in this
  account — answered from the index, not from a hundred clones.
- **Works from a Claude Code plugin.** Install it and your agent finds the index,
  reads it, keeps the repositories cloned for you in the background, and offers to
  rebuild it when it goes stale.
- **Commit it with your code.** The index is a small tree of TSV files. Whoever
  clones the repository gets it; no setup, no binary, no credential - and it
  records where each repository lives, so the plugin can fetch the code too.
- **Cheap to keep current.** Two staleness tiers: the forge's activity stamp
  decides what to pull, a content fingerprint decides what to describe. A
  repository with a commit a day gets pulled and costs nothing.
- **Bitbucket, GitHub, and a directory of checkouts**, behind one interface.
- **Any model.** The Anthropic API, any OpenAI-compatible endpoint (Ollama and
  vLLM included), or the Claude Code CLI you are already logged into.
- **Private by default.** Tokens live in the OS keychain, clones are shallow,
  and nothing but paths and a README ever reaches a model.

## Get started

**1. Install the plugin** — this is all a reader needs:

```
/plugin marketplace add branow/repomap
/plugin install repomap@repomap
```

**2. Build an index** — once per account, by whoever has access:

```
/repomap-build
```

It walks you through the rest: installing the CLI, adding the workspace and the
model backend, and where to write the tree — committed with the project, kept on
your machine, or published in a repository your team pulls.

**3. Ask.** No command, no slash, no flag:

> Which repository handles checkout, and where is the payment call made?

Teammates who pull a published index run `/repomap-config sync`; an index
committed under `.repomap/` is found on its own.

See [plugins/repomap](plugins/repomap/README.md) for what each skill does.

## Without the plugin

The tree is plain text, so any agent can read it and so can you:

```sh
grep -i checkout .repomap/acme/repos-active.tsv
grep -n router .repomap/acme/files/order-service.tsv
```

Driving the build yourself is four commands, and the first needs no credential:

```sh
brew install branow/tap/repomap

repomap workspace add local --forge local --dir ~/src --auth none
repomap backend add haiku --provider claudecode --model claude-haiku-4-5
repomap profile create dev --workspace local --backend haiku && repomap profile switch dev

repomap index
```

A real forge is the same shape: `repomap workspace add acme --forge github
--slug acme --auth token`.

On an account you do not own, start with `repomap index --dry-run`, which reports
the plan without cloning anything. Other install methods and every flag are in
the [CLI reference](docs/CLI.md).

## Documentation

| | |
|---|---|
| [plugins/repomap](plugins/repomap/README.md) | The Claude Code plugin: its four skills, how an index is found, how the clones are kept |
| [docs/CLI.md](docs/CLI.md) | Installing, every command and flag, scripting, exit codes, CI |
| [docs/DESIGN.md](docs/DESIGN.md) | What the index looks like, what keeps it cheap, adding a forge |
| [AGENTS.md](AGENTS.md) | The contract for an agent changing this repository |

## Development

```sh
go build ./...
go test ./...
```

The one structured-output call per repository goes through
[github.com/branow/gollm](https://github.com/branow/gollm), a separate module
that covers the Anthropic API, any OpenAI-compatible endpoint, and the Claude
Code CLI behind one interface.

## License

MIT. See [LICENSE](LICENSE).
