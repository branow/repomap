"use strict";
// Fetching a published tree, against a real local repository. No network: the
// remote is a directory, which is all git needs to prove the clone, the update
// and the recovery from a broken checkout.

const { test } = require("node:test");
const assert = require("node:assert");
const { spawnSync } = require("node:child_process");
const { mkdtempSync, mkdirSync, writeFileSync, existsSync, rmSync, readFileSync } = require("node:fs");
const { join } = require("node:path");
const { tmpdir } = require("node:os");

const git = require("../lib/git.js");

const HEADER = "workspace\tforge\taccount\trepos\tbuilt";

function run(argv, cwd) {
  const result = spawnSync("git", argv, { cwd, encoding: "utf8" });
  assert.equal(result.status, 0, `git ${argv.join(" ")}: ${result.stderr}`);
}

// A published index is a repository with the tree somewhere inside it.
function publish(rows) {
  const repo = mkdtempSync(join(tmpdir(), "repomap-remote-"));
  mkdirSync(join(repo, "index"), { recursive: true });
  writeFileSync(join(repo, "index", "workspaces.tsv"), [HEADER, ...rows].join("\n") + "\n");
  run(["init", "--initial-branch=main"], repo);
  run(["-c", "user.email=t@example.internal", "-c", "user.name=t", "add", "."], repo);
  run(["-c", "user.email=t@example.internal", "-c", "user.name=t", "commit", "-m", "index"], repo);
  return repo;
}

function root(remote) {
  const checkout = join(mkdtempSync(join(tmpdir(), "repomap-data-")), "clones", "team");
  return { name: "team", remote, checkout, path: join(checkout, "index"), branch: null };
}

test("a first sync clones the repository and lands the tree inside it", () => {
  const entry = root(publish(["platform\tgithub\tacme\t3\t2026-03-05T09:00:00Z"]));

  const result = git.pull(entry);

  assert.equal(result.ok, true, result.detail);
  assert.equal(result.action, "cloned");
  assert.ok(existsSync(join(entry.path, "workspaces.tsv")));
});

// The checkout is a copy of somebody else's index. Merging it would mean
// resolving a conflict in a file nobody here is allowed to have edited.
test("a second sync resets to the remote rather than merging", () => {
  const remote = publish(["platform\tgithub\tacme\t3\t2026-03-05T09:00:00Z"]);
  const entry = root(remote);
  git.pull(entry);
  writeFileSync(join(entry.path, "workspaces.tsv"), "local edit\n");

  writeFileSync(join(remote, "index", "workspaces.tsv"), HEADER + "\nplatform\tgithub\tacme\t9\t2026-04-01T09:00:00Z\n");
  run(["-c", "user.email=t@example.internal", "-c", "user.name=t", "commit", "-am", "rebuild"], remote);

  const result = git.pull(entry);

  assert.equal(result.ok, true, result.detail);
  assert.equal(result.action, "updated");
  assert.match(readFileSync(join(entry.path, "workspaces.tsv"), "utf8"), /\t9\t/);
});

// There is nothing in the checkout to lose, so a directory too broken to fetch
// is thrown away rather than reported as a failure somebody has to repair.
test("a broken checkout is recloned", () => {
  const entry = root(publish(["platform\tgithub\tacme\t3\t2026-03-05T09:00:00Z"]));
  git.pull(entry);
  rmSync(join(entry.checkout, ".git"), { recursive: true, force: true });

  const result = git.pull(entry);

  assert.equal(result.ok, true, result.detail);
  assert.match(result.action, /recloned/);
  assert.ok(existsSync(join(entry.path, "workspaces.tsv")));
});

test("an unreachable remote fails with git's own reason", () => {
  const entry = root(join(tmpdir(), "repomap-no-such-repo"));

  const result = git.pull(entry);

  assert.equal(result.ok, false);
  assert.equal(result.action, "could not clone");
  assert.ok(result.detail.length > 0);
});
