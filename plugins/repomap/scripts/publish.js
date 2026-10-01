#!/usr/bin/env node
"use strict";
// Publishes an index tree: commits it to a dated branch in the repository that
// carries it, pushes, and optionally opens a pull request.
//
// The push is part of this rather than something the developer does afterwards,
// because the repository being committed to is not necessarily one they keep: a
// build can write into a checkout the plugin cloned, where an unpushed commit
// is lost with the directory. For the same reason this reports whether the index
// reached the remote - a build whose push was refused published nothing.
//
// Usage: node publish.js --tree=PATH [--remote-name=origin] [--branch=NAME]
//                        [--pr] [--data-dir=PATH]

const { resolve } = require("node:path");
const { spawnSync } = require("node:child_process");
const args = require("./lib/args.js");
const roster = require("./lib/roster.js");

function git(argv, cwd) {
  const r = spawnSync("git", argv, {
    cwd,
    encoding: "utf8",
    env: { ...process.env, GIT_TERMINAL_PROMPT: "0", GCM_INTERACTIVE: "never" },
  });
  return {
    ok: r.status === 0,
    out: (r.stdout ?? "").trim(),
    detail: (r.stderr || r.error?.message || "").trim(),
  };
}

// branchName is dated rather than random, so a second run on the same day
// continues the same branch instead of opening a second review of one change.
function branchName(date = new Date()) {
  return `repomap-index-${date.toISOString().slice(0, 10)}`;
}

function main(argv) {
  const { flags } = args.parse(argv);
  const tree = resolve(args.value(flags, "tree") ?? ".");
  const remote = args.value(flags, "remote-name", "origin");
  const branch = args.value(flags, "branch") ?? branchName();

  if (!roster.isTree(tree)) {
    return fail(`${tree} is not an index tree: it has no ${roster.ROSTER_FILE}`);
  }
  // The repository root, which is not necessarily the tree: a tree usually sits
  // in a subdirectory of the repository that publishes it.
  const top = git(["rev-parse", "--show-toplevel"], tree);
  if (!top.ok) return fail(`${tree} is not inside a git repository`);
  const repo = top.out;

  const staged = git(["add", "--", tree], repo);
  if (!staged.ok) return fail(`could not stage the index: ${staged.detail}`);

  // Nothing to commit is a success, not a failure: a build that described
  // nothing leaves the tree byte-identical, and that is the common case.
  const pending = git(["diff", "--cached", "--quiet"], repo);
  if (pending.ok) {
    out("the index is unchanged; nothing to publish\n");
    return 0;
  }

  const current = git(["rev-parse", "--abbrev-ref", "HEAD"], repo);
  if (current.out !== branch) {
    // Continue today's branch when it exists, so a second run of the day builds
    // on the first rather than forking from it.
    const exists = git(["rev-parse", "--verify", "--quiet", branch], repo).ok;
    const switched = git(exists ? ["checkout", branch] : ["checkout", "-b", branch], repo);
    if (!switched.ok) return fail(`could not switch to ${branch}: ${switched.detail}`);
  }

  const committed = git(["commit", "-m", `chore: refresh the repository index`], repo);
  if (!committed.ok) return fail(`could not commit: ${committed.detail}`);

  const pushed = git(["push", "--set-upstream", remote, branch], repo);
  if (!pushed.ok) {
    return fail(
      `the commit was made but NOT pushed, so nothing is published: ${pushed.detail}\n` +
        `The commit is in ${repo} on ${branch}.`,
    );
  }
  out(`published ${branch} to ${remote}\n`);

  if (flags.pr) out(openPR(repo, branch));
  return 0;
}

// openPR shells out to whichever forge cli is installed. Both are optional: a
// failure here leaves a pushed branch anybody can open a request from by hand,
// so it is reported and never fatal.
const clis = [
  { bin: "gh", argv: (b) => ["pr", "create", "--fill", "--head", b] },
  { bin: "bkt", argv: (b) => ["pr", "create", "--source", b, "--title", "Refresh the repository index"] },
];

// installed probes by running the binary rather than by asking a shell, because
// `command -v` is a POSIX builtin and this has to work on Windows too.
function installed(bin) {
  const r = spawnSync(bin, ["--version"], { encoding: "utf8" });
  return !r.error;
}

function openPR(repo, branch) {
  for (const cli of clis) {
    if (!installed(cli.bin)) continue;
    const r = spawnSync(cli.bin, cli.argv(branch), { cwd: repo, encoding: "utf8" });
    if (r.status === 0) return `opened a pull request with ${cli.bin}\n${(r.stdout ?? "").trim()}\n`;
    return `${cli.bin} could not open a pull request: ${(r.stderr ?? "").trim()}\nOpen one from ${branch} by hand.\n`;
  }
  return `no gh or bkt on PATH; open a pull request from ${branch} by hand.\n`;
}

function out(text) {
  process.stdout.write(text);
}

function fail(message) {
  process.stderr.write(message + "\n");
  return 1;
}

if (require.main === module) {
  try {
    process.exit(main(process.argv.slice(2)));
  } catch (err) {
    if (err instanceof args.UsageError) {
      process.stderr.write(err.message + "\n");
      process.exit(1);
    }
    throw err;
  }
}

module.exports = { main, branchName };
