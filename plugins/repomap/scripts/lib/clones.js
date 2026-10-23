"use strict";
// Keeps the working trees an index points at. This is the half of the tool that
// turns a map into something you can read: the index says which repository and
// which file, and a clone is what lets you open it.
//
// It needs no forge API and no credential of its own. Every clone url it uses is
// a column in the catalog the build already wrote, so a reader who pulled a
// published index can fetch the code with nothing but git. That is why cloning
// lives here rather than only in the binary - a reader has no binary.
//
// The clones are a cache this plugin owns: a fetch is followed by a hard reset,
// and a directory too broken to fetch is removed and cloned again. Nothing of
// anybody's lives in them.

const { existsSync, mkdirSync, readdirSync, rmSync, statSync, writeFileSync, readFileSync } = require("node:fs");
const { join } = require("node:path");
const git = require("./git.js");
const roster = require("./roster.js");

// StaleAfterHours is how old a clone may be before a background sync pulls it
// again. Twelve hours means a machine left running overnight is current by
// morning, and a machine used all day pulls once.
const StaleAfterHours = 12;

// Concurrency bounds parallel git processes. Four saturates a normal connection
// without turning a session start into a fan of a hundred sockets.
const Concurrency = 4;

const LOCK = "sync.lock";
// A lock older than this was left by a run that was killed; a background sync
// has no cleanup handler that survives SIGKILL.
const LockStaleMinutes = 30;

// root is where this plugin keeps clones, and it is the single authority on that
// path: the build skill passes it to the binary with --clones, so the binary
// clones INTO here rather than into its own cache. One copy on disk, and
// uninstalling the plugin removes every byte it wrote.
function root(registry) {
  return join(registry.root, "clones");
}

function dirFor(registry, workspace, repo) {
  return join(root(registry), workspace, repo);
}

// A local-forge workspace has nothing to clone, by design: its repositories are
// checkouts that were already on disk, which the build read where they sat and
// never wrote to. There is no clone url to record and nothing for a sync to
// fetch, so one must not be reported as an index that needs rebuilding - that is
// a false alarm on every session start, about a problem no rebuild would fix.
function syncable(forge) {
  return forge !== "local";
}

// repos reads the catalog rows of one workspace: the name and the clone url, by
// header name rather than by column position so a build that adds a column does
// not silently shift what this reads.
//
// A row whose description is a placeholder in parentheses - `(archived)` - was
// skipped by the build, which never cloned it, so neither does this. Leaving it
// out here also lets prune remove a clone made before the repository retired.
function repos(treePath, workspace) {
  const file = join(treePath, workspace, "repos.tsv");
  if (!existsSync(file)) return [];

  const lines = readFileSync(file, "utf8").split("\n");
  const header = (lines.shift() ?? "").split("\t");
  const at = (cells, name) => cells[header.indexOf(name)] ?? "";

  const rows = [];
  for (const line of lines) {
    if (!line.trim() || line.startsWith("#")) continue;
    const cells = line.split("\t");
    if (!cells[0] || cells[0] === "repo") continue;
    if (skipped(at(cells, "description"))) continue;
    rows.push({ name: cells[0], remote: at(cells, "remote"), commit: at(cells, "commit") });
  }
  return rows;
}

function skipped(description) {
  return description.startsWith("(") && description.endsWith(")");
}

// plan decides what this run will touch. A repository with no clone url is
// reported rather than skipped silently: it means the index was built by a
// repomap older than the remote column, and the fix is a rebuild.
function plan(registry, treePath, workspace, { threshold = StaleAfterHours, only = null } = {}) {
  const work = [];
  const unreachable = [];

  for (const repo of repos(treePath, workspace)) {
    if (only && repo.name !== only) continue;
    if (!repo.remote) {
      unreachable.push(repo.name);
      continue;
    }
    const dir = dirFor(registry, workspace, repo.name);
    const action = actionFor(dir, threshold);
    if (action) work.push({ ...repo, dir, action });
  }
  return { work, unreachable };
}

// actionFor is the staleness decision, and the reason a session start is cheap:
// a clone pulled an hour ago is left alone entirely.
function actionFor(dir, threshold) {
  if (!existsSync(dir)) return "clone";
  if (!existsSync(join(dir, ".git"))) return "reclone";
  if (threshold <= 0) return "pull";
  return ageHours(dir) >= threshold ? "pull" : null;
}

// The age of a clone is when git last wrote to it, which a fetch touches even
// when it brings nothing new - so a repository nobody pushes to is still only
// probed once per threshold.
function ageHours(dir) {
  try {
    return (Date.now() - statSync(join(dir, ".git")).mtimeMs) / 3600000;
  } catch {
    return Infinity;
  }
}

// sync brings one workspace's clones up to date. Results are per repository: one
// repository nobody can reach must not stop the other two hundred.
async function sync(registry, treePath, workspace, opts = {}) {
  const log = opts.log;
  if (opts.forge && !syncable(opts.forge)) {
    return { synced: 0, failed: [], skipped: [], local: true };
  }
  const { work, unreachable } = plan(registry, treePath, workspace, opts);

  for (const name of unreachable) {
    log?.warn("sync", `${name} has no clone url in the index; rebuild it with /repomap-build`);
  }
  if (work.length === 0) return { synced: 0, failed: [], skipped: unreachable };

  // One probe before a hundred clones. The failure this catches - no network, no
  // VPN, a dead SSH key - fails every repository identically, so it is one
  // message rather than two hundred.
  const probe = git.reachable(work[0].remote);
  if (!probe.ok) {
    log?.error(
      "sync",
      `cannot reach ${work[0].remote}: check the network, the VPN, and that this ` +
        `machine's git credentials still work. No repositories were cloned.`,
    );
    return { synced: 0, failed: [], skipped: unreachable, access: work[0].remote };
  }

  log?.info("sync", `${work.length} ${work.length === 1 ? "repository" : "repositories"} to fetch`);

  const failed = [];
  let synced = 0;
  const queue = [...work];
  const limit = Math.min(opts.concurrency || Concurrency, 8);

  await Promise.all(
    Array.from({ length: Math.min(limit, queue.length) }, async () => {
      for (;;) {
        const item = queue.shift();
        if (!item) return;
        const result = one(item);
        if (result.ok) synced++;
        else {
          failed.push(item.name);
          log?.warn("sync", `${item.name}: ${result.detail}`);
        }
      }
    }),
  );

  if (synced > 0) log?.info("sync", `${synced} up to date`);
  if (failed.length > 0) {
    log?.warn("sync", `${failed.length} could not be fetched: ${failed.join(", ")}`);
  }
  return { synced, failed, skipped: unreachable };
}

function one(item) {
  if (item.action === "reclone") rmSync(item.dir, { recursive: true, force: true });
  if (item.action === "pull") return git.refresh(item.dir);
  mkdirSync(join(item.dir, ".."), { recursive: true });
  return git.shallowClone(item.remote, item.dir);
}

// prune removes clones of repositories the catalog no longer lists. Without it a
// machine accumulates the clone of every repository ever retired, and nothing
// else would ever delete them.
function prune(registry, treePath, workspace) {
  const dir = join(root(registry), workspace);
  if (!existsSync(dir)) return [];
  const known = new Set(repos(treePath, workspace).map((r) => r.name));
  if (known.size === 0) return [];

  const removed = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory() || known.has(entry.name)) continue;
    rmSync(join(dir, entry.name), { recursive: true, force: true });
    removed.push(entry.name);
  }
  return removed;
}

// held reports how many of a workspace's repositories are on disk, so `list` can
// say whether the code behind an index is actually reachable. A local-forge
// workspace reports `local` instead of 0 of N: its code is on disk somewhere this
// plugin does not manage, and a count would read as code missing.
function held(registry, treePath, workspace, forge) {
  const all = repos(treePath, workspace);
  if (forge && !syncable(forge)) return { local: true, total: all.length };
  let present = 0;
  for (const repo of all) {
    if (existsSync(join(dirFor(registry, workspace, repo.name), ".git"))) present++;
  }
  return { present, total: all.length };
}

// A lock, so two sessions starting at once do not fetch the same repositories
// into the same directories. A stale lock is taken over rather than respected:
// a background run killed with the terminal leaves one behind, and a plugin that
// then refused to sync forever would be worse than one that overlapped once.
function lock(registry) {
  const file = join(registry.root, LOCK);
  try {
    mkdirSync(registry.root, { recursive: true });
    if (existsSync(file)) {
      const age = (Date.now() - statSync(file).mtimeMs) / 60000;
      if (age < LockStaleMinutes) return null;
    }
    writeFileSync(file, String(process.pid) + "\n");
    return () => {
      try {
        rmSync(file, { force: true });
      } catch {}
    };
  } catch {
    return null;
  }
}

// workspaces lists the workspaces of every tree this machine reads, so a sync
// with no arguments covers all of them.
function workspacesOf(trees) {
  const out = [];
  for (const tree of trees) {
    if (!tree.present) continue;
    for (const ws of roster.workspaces(tree.path)) {
      out.push({ tree: tree.name, path: tree.path, workspace: ws.workspace, forge: ws.forge });
    }
  }
  return out;
}

module.exports = {
  syncable,
  StaleAfterHours,
  Concurrency,
  root,
  dirFor,
  repos,
  plan,
  sync,
  prune,
  held,
  lock,
  workspacesOf,
};
