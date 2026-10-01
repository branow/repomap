#!/usr/bin/env node
"use strict";
// CLI behind the repomap skills: what trees this machine can read, and the few
// facts about them a person ever sets. The skills run this and relay what it
// prints, so where an index lives is a fact on disk rather than something a
// model decided.
//
// Usage: node roots.js [--data-dir=PATH] <command>
//   list [--json]                    trees this machine can read (default)
//   add [<path>] [--name=N] [--remote=URL] [--subdir=REL] [--branch=B]
//   remove <name>
//   remind <hours> | remind off
//   sync [<name>]                    fetch the published index trees
//   clones [<workspace>]             fetch the repositories an index points at
//
// `add` with no path registers this plugin's own data directory, which is where
// a build for one person lands. `add --remote=URL` registers a published tree
// and prints the directory `sync` will clone it into.

const { basename, join } = require("node:path");
const args = require("./lib/args.js");
const git = require("./lib/git.js");
const roster = require("./lib/roster.js");
const clones = require("./lib/clones.js");
const { createRegistry } = require("./lib/registry.js");

const COMMANDS = {
  list: list,
  add: add,
  remove: drop,
  remind: remind,
  sync: sync,
  clones: cloneRoots,
};

function main(argv) {
  const { flags, positionals } = args.parse(argv);
  const registry = createRegistry(flags["data-dir"] || undefined);
  const [command = "list", ...rest] = positionals;

  const run = COMMANDS[command];
  if (!run) return fail(`not a command: "${command}". One of: ${Object.keys(COMMANDS).join(", ")}`);
  try {
    run(registry, rest, flags);
  } catch (err) {
    if (err instanceof args.UsageError) return fail(err.message);
    throw err;
  }
}

// Everything this machine can read, discovered tree first: it is the one the
// current session is most likely to mean, and it is the one nobody registered.
function list(registry, _rest, flags) {
  const found = resolveAll(registry);
  if (flags.json) return out(JSON.stringify(found, null, 2) + "\n");

  const threshold = registry.remindAfterHours();
  out(`reminder: ${threshold ? `on, once a tree is older than ${threshold} hours` : "off"}\n`);
  if (found.trees.length === 0) {
    return out(
      "no index tree on this machine\n" +
        "  build one with /repomap-build, or register a published one with " +
        "/repomap-config add --remote=<git url>\n",
    );
  }
  for (const tree of found.trees) out(describeTree(tree));
}

// A tree the skills can hand to Read and Grep: its path, where it came from,
// and what it holds. A registered path that is not there yet is reported rather
// than hidden - it is the normal state of a published tree before the first
// sync, and the fix differs from the fix for a wrong path.
function resolveAll(registry) {
  const trees = [];
  const seen = new Set();

  const discovered = roster.discover(process.cwd());
  if (discovered) {
    trees.push(inspect({ name: "repo", path: discovered, origin: "repo" }, registry));
    seen.add(discovered);
  }
  for (const root of registry.read().roots) {
    if (seen.has(root.path)) continue;
    seen.add(root.path);
    trees.push(inspect({ ...root, origin: root.remote ? "remote" : "local" }, registry));
  }
  return { cwd: process.cwd(), trees };
}

function inspect(root, registry) {
  const present = roster.isTree(root.path);
  const workspaces = (present ? roster.workspaces(root.path) : []).map((ws) => ({
    ...ws,
    clones: clones.held(registry, root.path, ws.workspace),
    clonesDir: join(clones.root(registry), ws.workspace),
  }));
  const stalest = roster.oldest(workspaces);
  return {
    ...root,
    present,
    workspaces,
    oldestBuilt: stalest ? new Date(stalest.ms).toISOString() : null,
    ageHours: stalest ? roster.ageHours(stalest.ms) : null,
  };
}

function describeTree(tree) {
  const head = `${tree.name}  ${tree.path}${tree.remote ? `  <- ${tree.remote}` : ""}\n`;
  if (!tree.present) {
    return (
      head +
      (tree.remote
        ? "  not cloned yet; run /repomap-config sync\n"
        : "  nothing there; the path is wrong, or the build has not run yet\n")
    );
  }
  if (tree.workspaces.length === 0) {
    return head + "  no workspaces.tsv; built by an older repomap, read the directories directly\n";
  }
  return (
    head +
    tree.workspaces
      .map((ws) => {
        const holds = Object.entries(ws.counts)
          .map(([kind, n]) => `${n} ${count(kind, n)}`)
          .join(", ");
        const built = ws.built
          ? `${ws.built.slice(0, 10)}, ${roster.describeAge(roster.ageHours(Date.parse(ws.built)))} ago`
          : "unknown";
        return (
          `  ${ws.workspace}  ${ws.forge} ${ws.account}  ${holds || "empty"}  built ${built}\n` +
          `    clones: ${ws.clones.present}/${ws.clones.total} on disk at ${ws.clonesDir}\n`
        );
      })
      .join("")
  );
}

// Registering is the only way a tree outside the current repository becomes
// readable, and it is deliberately the same command for both kinds: a directory
// somebody built into, and a git repository somebody publishes.
function add(registry, rest, flags) {
  const remote = args.value(flags, "remote");
  const name = args.value(flags, "name") ?? (remote ? repoName(remote) : "local");
  const path = rest[0] || (remote ? registry.clonePath(name) : registry.localTreePath());

  const entry = registry.add({
    name,
    path,
    remote,
    subdir: args.value(flags, "subdir"),
    branch: args.value(flags, "branch"),
  });

  out(`registered ${entry.name}\n  tree: ${entry.path}\n`);
  if (entry.remote) out(`  clone: ${entry.checkout} from ${entry.remote}\n  run /repomap-config sync to fetch it\n`);
  else if (!roster.isTree(entry.path)) out(`  nothing there yet; build into it with: repomap index --out ${entry.path}\n`);
}

function drop(registry, rest) {
  const name = rest[0];
  if (!name) return fail("remove needs a name; run `list` to see them");
  if (!registry.remove(name)) return fail(`no root named "${name}"`);
  out(`removed ${name}\n`);
}

// The reminder is off until somebody asks for it. New repositories appear and
// old ones are retired on the scale of weeks, so a few days is the useful
// threshold here.
function remind(registry, rest) {
  const value = rest[0];
  if (value === undefined) return fail("remind needs a number of hours, or `off`");
  if (/^off$/i.test(value)) {
    registry.setRemindAfterHours(null);
    return out("reminder: off\n");
  }
  const hours = Number(value);
  if (!(hours > 0)) return fail(`not a threshold: "${value}". Pass hours (e.g. 168), or "off"`);
  registry.setRemindAfterHours(hours);
  out(`reminder: on, once a tree is older than ${hours} hours\n`);
}

// Roster columns are named in the plural ("repos", "files"), which reads wrong
// for a workspace holding one of something.
function count(kind, n) {
  return n === 1 ? kind.replace(/s$/, "") : kind;
}

// Fetches the published trees: every registered remote, or one by name. A tree
// committed in a repository arrives with the checkout and one built here has
// nothing to fetch, so neither is ever a candidate.
function sync(registry, rest) {
  const named = rest[0];
  const remotes = registry
    .read()
    .roots.filter((root) => root.remote && (!named || root.name === named));

  if (remotes.length === 0) {
    return fail(named ? `no root named "${named}" with a remote` : "no published tree is registered");
  }
  for (const root of remotes) {
    const result = git.pull(root);
    out(`${root.name}: ${result.action}\n`);
    if (!result.ok) fail(result.detail);
    if (!roster.isTree(root.path)) {
      out(`  no index at ${root.path}; the --subdir is wrong for this repository\n`);
    }
  }
}

// Reports where the clones are and how many are there. Fetching them is sync.js,
// which the session-start hook also runs, so there is one implementation of it.
function cloneRoots(registry, rest) {
  const named = rest[0];
  const targets = clones.workspacesOf(resolveAll(registry).trees).filter(
    (t) => !named || t.workspace === named,
  );
  if (targets.length === 0) {
    return fail(named ? `no workspace named "${named}"` : "no index tree on this machine");
  }
  for (const target of targets) {
    const held = clones.held(registry, target.path, target.workspace);
    out(
      `${target.workspace}  ${held.present}/${held.total} on disk\n` +
        `  ${join(clones.root(registry), target.workspace)}\n`,
    );
  }
}

// git@host:owner/repo.git and https://host/owner/repo both name the repo last.
function repoName(remote) {
  return basename(remote.replace(/\.git$/, "").replace(/\/+$/, "")) || "remote";
}

function out(text) {
  process.stdout.write(text);
}

function fail(message) {
  process.stderr.write(message + "\n");
  process.exit(1);
}

if (require.main === module) main(process.argv.slice(2));

module.exports = { main, resolveAll };
