"use strict";
// What a tree says about itself, read with nothing but the files: the roster
// the repomap CLI writes at the root, and the walk that finds a tree committed
// beside the project.

const { test } = require("node:test");
const assert = require("node:assert");
const { mkdtempSync, mkdirSync, writeFileSync } = require("node:fs");
const { join } = require("node:path");
const { tmpdir } = require("node:os");

const roster = require("../lib/roster.js");

const HEADER = "workspace\tforge\taccount\trepos\tactive\tfiles\tbuilt";

function tree(rows, dir = mkdtempSync(join(tmpdir(), "repomap-tree-"))) {
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "workspaces.tsv"), [HEADER, ...rows].join("\n") + "\n");
  return dir;
}

test("a tree's workspaces come back with their counts and their stamp", () => {
  const dir = tree(["platform\tgithub\tacme\t148\t96\t41203\t2026-03-05T09:00:00Z"]);
  const [ws] = roster.workspaces(dir);

  assert.equal(ws.workspace, "platform");
  assert.equal(ws.forge, "github");
  assert.equal(ws.account, "acme");
  assert.deepEqual(ws.counts, { repos: 148, active: 96, files: 41203 });
  assert.equal(ws.dir, join(dir, "platform"));
});

// The age of a tree is the age of its stalest workspace. Reporting the newest
// build would hide exactly the workspace nobody has rebuilt.
test("the oldest build is what dates a tree", () => {
  const dir = tree([
    "platform\tgithub\tacme\t148\t96\t41203\t2026-03-05T09:00:00Z",
    "tools\tbitbucket\tacme-tools\t12\t4\t900\t2025-11-02T09:00:00Z",
  ]);
  const stalest = roster.oldest(roster.workspaces(dir));

  assert.equal(stalest.row.workspace, "tools");
});

// A tree written by a repomap that reports a count this plugin has never heard
// of must still show it. The counts are whatever columns the roster carries.
test("an unknown count column is reported, not dropped", () => {
  const dir = mkdtempSync(join(tmpdir(), "repomap-tree-"));
  writeFileSync(
    join(dir, "workspaces.tsv"),
    "workspace\tforge\taccount\trepos\tlanguages\tbuilt\n" +
      "platform\tgithub\tacme\t3\t7\t2026-03-05T09:00:00Z\n",
  );

  assert.deepEqual(roster.workspaces(dir)[0].counts, { repos: 3, languages: 7 });
});

// A tree from before the roster existed is still perfectly readable: the
// catalogs are there. Treating it as an error would break the read path for
// every index built by an older release.
test("a tree with no roster reads as empty rather than failing", () => {
  const dir = mkdtempSync(join(tmpdir(), "repomap-tree-"));
  mkdirSync(join(dir, "platform"), { recursive: true });

  assert.deepEqual(roster.workspaces(dir), []);
  assert.equal(roster.isTree(dir), false);
});

// Discovery is what keeps the committed-tree case at zero configuration.
test("a tree committed in the repository is found from a subdirectory", () => {
  const repo = mkdtempSync(join(tmpdir(), "repomap-repo-"));
  tree(["platform\tgithub\tacme\t1\t1\t9\t2026-03-05T09:00:00Z"], join(repo, ".repomap"));
  const deep = join(repo, "src", "api");
  mkdirSync(deep, { recursive: true });

  assert.equal(roster.discover(deep), join(repo, ".repomap"));
});

test("discovery stops rather than walking to the filesystem root forever", () => {
  const empty = mkdtempSync(join(tmpdir(), "repomap-empty-"));

  assert.equal(roster.discover(empty), null);
});
