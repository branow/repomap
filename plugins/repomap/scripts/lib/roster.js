"use strict";
// Reads what an index tree says about itself. The tree is written by the repomap
// CLI and carries workspaces.tsv at its root - one row per workspace, with the
// counts and the build stamp - so nothing here needs the CLI, a forge, or any
// configuration to describe a tree it is handed.
//
// This is the whole reason the plugin can be installed by people who will never
// build an index: reading is files, not tooling.

const { existsSync, readFileSync, statSync } = require("node:fs");
const { join, parse: parsePath, resolve } = require("node:path");

const ROSTER_FILE = "workspaces.tsv";
// The conventional name of a tree committed beside the project it describes,
// which is where the repomap CLI writes one when --out is not given.
const REPO_TREE_DIR = ".repomap";
const HOUR_MS = 3600 * 1000;

// A tree is a directory holding a roster. Anything else is a path someone
// mistyped, or a clone that has not been pulled yet.
function isTree(dir) {
  return Boolean(dir) && existsSync(join(dir, ROSTER_FILE));
}

// The workspaces in one tree, in the roster's own order. A tree that exists but
// carries no roster reads as empty rather than as an error: it was built by a
// repomap older than the roster, and its catalogs are still perfectly readable.
function workspaces(dir) {
  const file = join(dir, ROSTER_FILE);
  if (!existsSync(file)) return [];

  const lines = readFileSync(file, "utf8").split("\n");
  const header = (lines.shift() ?? "").split("\t");
  const rows = [];
  for (const line of lines) {
    if (!line.trim() || line.startsWith("#")) continue;
    const cells = line.split("\t");
    if (cells.length < 2) continue;
    const at = (name) => cells[header.indexOf(name)] ?? "";
    rows.push({
      workspace: cells[0],
      forge: cells[1],
      account: cells[2] ?? "",
      counts: counts(header, cells),
      built: at("built") || null,
      dir: join(dir, cells[0]),
    });
  }
  return rows;
}

// Every column between the identity columns and the build stamp is a count, so
// a tree written by a repomap that reports a new one shows it here without this
// plugin being taught its name.
function counts(header, cells) {
  const tally = {};
  for (let i = 3; i < header.length; i++) {
    if (header[i] === "built") continue;
    const n = Number(cells[i]);
    if (Number.isFinite(n) && n > 0) tally[header[i]] = n;
  }
  return tally;
}

// The age of a tree is the age of its STALEST workspace, not its newest. A tree
// whose second workspace was last built in the spring is out of date, and
// reporting the most recent build would hide exactly that.
function oldest(rows) {
  let worst = null;
  for (const row of rows) {
    const ms = Date.parse(row.built ?? "");
    if (!Number.isFinite(ms)) continue;
    if (worst === null || ms < worst.ms) worst = { ms, row };
  }
  return worst;
}

function ageHours(ms, now = Date.now()) {
  return (now - ms) / HOUR_MS;
}

function describeAge(hours) {
  const days = Math.floor(hours / 24);
  if (days > 0) return `${days} day${days === 1 ? "" : "s"}`;
  return `${Math.floor(hours)} hour${Math.floor(hours) === 1 ? "" : "s"}`;
}

// A tree committed beside the project, found by walking up from where the
// session is running. Nearest wins: a checkout inside a checkout describes
// itself, not its parent.
//
// Discovery is what keeps the ordinary case at zero configuration - the tree is
// in the repo, so nobody has to register anything for the read skill to work.
function discover(startDir) {
  let dir = resolve(startDir);
  for (;;) {
    const candidate = join(dir, REPO_TREE_DIR);
    if (isTree(candidate)) return candidate;
    const parent = parsePath(dir).dir;
    if (!parent || parent === dir) return null;
    dir = parent;
  }
}

// When the tree's files were last touched, for a tree with no roster to stamp.
function modifiedAt(dir) {
  try {
    return statSync(dir).mtimeMs;
  } catch {
    return null;
  }
}

module.exports = {
  ROSTER_FILE,
  REPO_TREE_DIR,
  isTree,
  workspaces,
  oldest,
  ageHours,
  describeAge,
  discover,
  modifiedAt,
};
