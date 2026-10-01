"use strict";
// The clone sync: the half of the plugin that turns a map into something you can
// read. Every test here drives real git against a local remote - no network.

const { test } = require("node:test");
const assert = require("node:assert");
const { mkdtempSync, mkdirSync, writeFileSync, existsSync, readdirSync } = require("node:fs");
const { join } = require("node:path");
const { tmpdir } = require("node:os");
const { spawnSync } = require("node:child_process");

const clones = require("../lib/clones.js");
const { createLog } = require("../lib/log.js");
const { createRegistry } = require("../lib/registry.js");

function git(argv, cwd) {
  const r = spawnSync("git", argv, { cwd, encoding: "utf8" });
  assert.equal(r.status, 0, `git ${argv.join(" ")}: ${r.stderr}`);
}

// publish makes a real git repository that can be cloned from, so the sync is
// exercised against git itself rather than against a stub of it.
function publish(name, files = { "main.go": "package main\n" }) {
  const dir = mkdtempSync(join(tmpdir(), `repomap-remote-${name}-`));
  git(["init", "-q", "--initial-branch=main", "."], dir);
  for (const [path, body] of Object.entries(files)) writeFileSync(join(dir, path), body);
  git(["add", "-A"], dir);
  git(["-c", "user.email=t@e.c", "-c", "user.name=t", "commit", "-qm", "init"], dir);
  return dir;
}

// tree writes the index a reader would have pulled: a roster and a catalog whose
// rows carry the clone urls.
function tree(rows) {
  const dir = mkdtempSync(join(tmpdir(), "repomap-tree-"));
  mkdirSync(join(dir, "ws"), { recursive: true });
  writeFileSync(
    join(dir, "workspaces.tsv"),
    "workspace\tforge\taccount\trepos\tactive\tfiles\tbuilt\n" +
      `ws\tgithub\tacme\t${rows.length}\t${rows.length}\t1\t2026-10-01T00:00:00Z\n`,
  );
  writeFileSync(
    join(dir, "ws", "repos.tsv"),
    "repo\tfiles\tsize\tcommit\tremote\tactivity\tfingerprint\tdescription\n" +
      rows
        .map((r) => `${r.name}\t1\t1 KB\t2026-01-01\t${r.remote}\tx\thash\t${r.name} does a thing`)
        .join("\n") +
      "\n",
  );
  return dir;
}

function registry() {
  return createRegistry(mkdtempSync(join(tmpdir(), "repomap-data-")));
}

test("the index alone is enough to fetch the code", async () => {
  const remote = publish("api");
  const reg = registry();
  const path = tree([{ name: "api", remote }]);

  const result = await clones.sync(reg, path, "ws");

  assert.equal(result.synced, 1);
  assert.deepEqual(result.failed, []);
  // The point of the whole feature: real source on disk, from the index's own
  // column, with no forge api and no credential.
  assert.ok(existsSync(join(clones.dirFor(reg, "ws", "api"), "main.go")));
});

// A clone pulled an hour ago is left alone, which is what keeps a session start
// cheap on an account of two hundred repositories.
test("a fresh clone is not fetched again", async () => {
  const reg = registry();
  const path = tree([{ name: "api", remote: publish("api") }]);

  assert.equal((await clones.sync(reg, path, "ws")).synced, 1);
  assert.equal((await clones.sync(reg, path, "ws")).synced, 0);
  // Asking for one repository means asking now, so a zero threshold still works.
  assert.equal((await clones.sync(reg, path, "ws", { threshold: 0 })).synced, 1);
});

test("a new commit upstream is picked up", async () => {
  const remote = publish("api");
  const reg = registry();
  const path = tree([{ name: "api", remote }]);
  await clones.sync(reg, path, "ws");

  writeFileSync(join(remote, "router.go"), "package main\n");
  git(["add", "-A"], remote);
  git(["-c", "user.email=t@e.c", "-c", "user.name=t", "commit", "-qm", "add a router"], remote);

  await clones.sync(reg, path, "ws", { threshold: 0 });
  assert.ok(existsSync(join(clones.dirFor(reg, "ws", "api"), "router.go")));
});

// The clones are a cache this plugin owns, so an edit in one is damage to repair.
test("a local edit is reset rather than merged", async () => {
  const remote = publish("api");
  const reg = registry();
  const path = tree([{ name: "api", remote }]);
  await clones.sync(reg, path, "ws");

  const file = join(clones.dirFor(reg, "ws", "api"), "main.go");
  writeFileSync(file, "// somebody edited the cache\n");
  await clones.sync(reg, path, "ws", { threshold: 0 });

  assert.equal(require("node:fs").readFileSync(file, "utf8"), "package main\n");
});

// A directory that exists and is not a repository is what a killed clone leaves.
test("a half-written clone is replaced", async () => {
  const reg = registry();
  const path = tree([{ name: "api", remote: publish("api") }]);
  const dir = clones.dirFor(reg, "ws", "api");
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "junk.txt"), "x");

  assert.equal((await clones.sync(reg, path, "ws")).synced, 1);
  assert.ok(existsSync(join(dir, "main.go")));
  assert.ok(!existsSync(join(dir, "junk.txt")));
});

// One unreachable remote fails the whole run up front, because the cause - no
// network, no vpn, a dead key - fails every repository identically.
test("an unreachable remote is one message, not one per repository", async () => {
  const reg = registry();
  const log = createLog(reg.root);
  const path = tree([
    { name: "a", remote: "/nonexistent/a.git" },
    { name: "b", remote: "/nonexistent/b.git" },
    { name: "c", remote: "/nonexistent/c.git" },
  ]);

  const result = await clones.sync(reg, path, "ws", { log });

  assert.ok(result.access, "the run did not report an access failure");
  assert.equal(result.synced, 0);
  const { entries } = log.unread("error");
  assert.equal(entries.length, 1, "one access failure produced more than one message");
});

// A row with no clone url means the index predates the remote column, and the
// fix is a rebuild - so say that rather than failing silently.
test("a repository with no clone url is reported", async () => {
  const reg = registry();
  const log = createLog(reg.root);
  const path = tree([{ name: "api", remote: publish("api") }, { name: "old", remote: "" }]);

  const result = await clones.sync(reg, path, "ws", { log });

  assert.deepEqual(result.skipped, ["old"]);
  assert.equal(result.synced, 1, "the reachable repository was not fetched");
  assert.match(log.unread("warn").entries.map((e) => e.message).join("\n"), /no clone url/);
});

test("one repository can be fetched by name", async () => {
  const reg = registry();
  const path = tree([
    { name: "api", remote: publish("api") },
    { name: "web", remote: publish("web") },
  ]);

  const result = await clones.sync(reg, path, "ws", { only: "api" });

  assert.equal(result.synced, 1);
  assert.ok(existsSync(clones.dirFor(reg, "ws", "api")));
  assert.ok(!existsSync(clones.dirFor(reg, "ws", "web")));
});

// Nothing else would ever delete these, so a machine would otherwise accumulate
// the clone of every repository ever retired.
test("prune removes clones the catalog no longer lists", async () => {
  const reg = registry();
  const path = tree([{ name: "api", remote: publish("api") }]);
  await clones.sync(reg, path, "ws");
  mkdirSync(join(clones.root(reg), "ws", "retired", ".git"), { recursive: true });

  assert.deepEqual(clones.prune(reg, path, "ws"), ["retired"]);
  assert.deepEqual(readdirSync(join(clones.root(reg), "ws")), ["api"]);
});

// A prune that ran against an unreadable catalog would delete every clone.
test("prune does nothing when the catalog cannot be read", async () => {
  const reg = registry();
  mkdirSync(join(clones.root(reg), "ws", "api", ".git"), { recursive: true });

  assert.deepEqual(clones.prune(reg, mkdtempSync(join(tmpdir(), "empty-")), "ws"), []);
  assert.ok(existsSync(join(clones.root(reg), "ws", "api")));
});

test("two sessions starting together do not sync at once", () => {
  const reg = registry();
  const release = clones.lock(reg);
  assert.ok(release, "the first run did not take the lock");
  assert.equal(clones.lock(reg), null, "a second run ran anyway");
  release();
  assert.ok(clones.lock(reg), "the lock was not released");
});

test("held reports whether the code behind an index is on disk", async () => {
  const reg = registry();
  const path = tree([
    { name: "api", remote: publish("api") },
    { name: "web", remote: publish("web") },
  ]);
  assert.deepEqual(clones.held(reg, path, "ws"), { present: 0, total: 2 });
  await clones.sync(reg, path, "ws", { only: "api" });
  assert.deepEqual(clones.held(reg, path, "ws"), { present: 1, total: 2 });
});

// Read by header name, so a build that adds a column does not shift what this
// reads out from under it.
test("the catalog is parsed by column name, not position", () => {
  const dir = mkdtempSync(join(tmpdir(), "repomap-tree-"));
  mkdirSync(join(dir, "ws"), { recursive: true });
  writeFileSync(
    join(dir, "ws", "repos.tsv"),
    "repo\tdescription\tremote\tfiles\n" + "api\ta thing\tgit@example.internal:a/api.git\t12\n",
  );
  assert.equal(clones.repos(dir, "ws")[0].remote, "git@example.internal:a/api.git");
});
