"use strict";
// Publishing an index: the step that makes a team's tree actually reach them.
// Driven against a real local remote, because what is worth pinning is that a
// push either happened or was reported as not having happened.

const { test } = require("node:test");
const assert = require("node:assert");
const { mkdtempSync, mkdirSync, writeFileSync, cpSync } = require("node:fs");
const { join } = require("node:path");
const { tmpdir } = require("node:os");
const { spawnSync } = require("node:child_process");

const { main, branchName } = require("../publish.js");

function git(argv, cwd) {
  const r = spawnSync("git", argv, { cwd, encoding: "utf8" });
  return { ok: r.status === 0, out: (r.stdout ?? "").trim() };
}

// A repository with a real remote, holding an index tree in a subdirectory -
// the shape a published index actually has.
function published() {
  const bare = mkdtempSync(join(tmpdir(), "repomap-bare-"));
  git(["init", "-q", "--bare", "--initial-branch=main", "."], bare);
  const work = mkdtempSync(join(tmpdir(), "repomap-work-"));
  git(["clone", "-q", bare, "."], work);
  git(["-c", "user.email=t@e.c", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"], work);
  git(["push", "-q", "origin", "HEAD:main"], work);
  git(["config", "user.email", "t@e.c"], work);
  git(["config", "user.name", "t"], work);
  return { bare, work };
}

function tree(dir, note = "x") {
  mkdirSync(join(dir, "ws"), { recursive: true });
  writeFileSync(
    join(dir, "workspaces.tsv"),
    "workspace\tforge\taccount\trepos\tactive\tfiles\tbuilt\n" +
      `ws\tgithub\tacme\t1\t1\t1\t2026-10-01T00:00:00Z\n`,
  );
  writeFileSync(join(dir, "README.md"), note);
  return dir;
}

test("the index reaches the remote on a dated branch", () => {
  const { bare, work } = published();
  const dir = tree(join(work, ".repomap"));

  assert.equal(main([`--tree=${dir}`]), 0);

  const heads = git(["ls-remote", "--heads", bare], work).out;
  assert.match(heads, new RegExp(branchName()));
});

// A build that described nothing leaves the tree byte-identical, and that is the
// common case, not a failure.
test("an unchanged tree publishes nothing and succeeds", () => {
  const { work } = published();
  const dir = tree(join(work, ".repomap"));
  assert.equal(main([`--tree=${dir}`]), 0);
  assert.equal(main([`--tree=${dir}`]), 0);
});

// A second run the same day continues the branch rather than opening a second
// review of one change.
test("a second run the same day continues the same branch", () => {
  const { bare, work } = published();
  const dir = tree(join(work, ".repomap"));
  main([`--tree=${dir}`]);
  writeFileSync(join(dir, "README.md"), "changed");
  main([`--tree=${dir}`]);

  const heads = git(["ls-remote", "--heads", bare], work).out;
  assert.equal(heads.split("\n").filter((l) => l.includes("repomap-index")).length, 1);
});

test("a directory that is not an index tree is refused", () => {
  const { work } = published();
  assert.equal(main([`--tree=${work}`]), 1);
});

test("a tree outside any repository is refused", () => {
  const dir = tree(mkdtempSync(join(tmpdir(), "repomap-loose-")));
  assert.equal(main([`--tree=${dir}`]), 1);
});

// The push is the publish. A commit that did not reach the remote published
// nothing, and must not be reported as success.
test("a refused push is a failed publish", () => {
  const { work } = published();
  const dir = tree(join(work, ".repomap"));
  git(["remote", "set-url", "origin", "/nonexistent/remote.git"], work);
  assert.equal(main([`--tree=${dir}`]), 1);
});

test("the branch name is dated, so it is stable within a day", () => {
  const day = new Date("2026-10-01T23:59:00Z");
  assert.equal(branchName(day), "repomap-index-2026-10-01");
  assert.equal(branchName(new Date("2026-10-01T00:00:01Z")), branchName(day));
});
