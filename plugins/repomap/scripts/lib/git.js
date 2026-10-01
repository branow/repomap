"use strict";
// Every git command this plugin runs. They live here rather than in a skill so
// fetching is one approval and one deterministic path, not a model composing git
// from a description.

const { spawnSync } = require("node:child_process");
const { existsSync, mkdirSync, rmSync } = require("node:fs");
const { dirname } = require("node:path");

function run(argv, cwd) {
  const result = spawnSync("git", argv, {
    cwd,
    encoding: "utf8",
    // Nothing here may stop for a passphrase: the session-start sync runs with
    // nobody watching, and a git that prompts hangs it until the session ends.
    env: { ...process.env, GIT_TERMINAL_PROMPT: "0", GCM_INTERACTIVE: "never" },
  });
  return {
    ok: result.status === 0,
    detail: (result.stderr || result.error?.message || "").trim(),
  };
}

// Brings one root's checkout to the remote's current state. A checkout that is
// there is RESET, never merged: it is a copy of somebody else's index and
// nothing local should ever have edited it. A checkout too broken to fetch is
// thrown away and cloned again, because there is nothing in it to lose.
function pull(root) {
  if (!existsSync(root.checkout)) return clone(root);

  const branch = root.branch ? [root.branch] : [];
  const fetched = run(["fetch", "--depth", "1", "origin", ...branch], root.checkout);
  if (!fetched.ok) {
    rmSync(root.checkout, { recursive: true, force: true });
    const recloned = clone(root);
    return { ...recloned, action: recloned.ok ? "recloned; the checkout was broken" : recloned.action };
  }

  const reset = run(["reset", "--hard", "FETCH_HEAD"], root.checkout);
  return { ok: reset.ok, action: reset.ok ? "updated" : "could not update", detail: reset.detail };
}

function clone(root) {
  mkdirSync(dirname(root.checkout), { recursive: true });
  const branch = root.branch ? ["--branch", root.branch] : [];
  const result = run(["clone", "--depth", "1", ...branch, root.remote, root.checkout]);
  return { ok: result.ok, action: result.ok ? "cloned" : "could not clone", detail: result.detail };
}

// shallowClone fetches one repository's tip. Shallow and single-branch because
// the reader wants to read the current code, not its history, and a full clone of
// a large account is gigabytes bought for a question nobody asks here.
function shallowClone(remote, dir) {
  mkdirSync(dirname(dir), { recursive: true });
  return run(["clone", "--depth=1", "--single-branch", "--no-tags", remote, dir]);
}

// refresh brings an existing shallow clone to the remote's tip. It resets rather
// than merges: these clones are a cache, so a local change in one is damage to
// repair, not work to keep.
function refresh(dir) {
  const fetched = run(["fetch", "--depth=1", "--no-tags", "origin"], dir);
  if (!fetched.ok) return fetched;
  const head = spawnSync("git", ["rev-parse", "--abbrev-ref", "origin/HEAD"], {
    cwd: dir,
    encoding: "utf8",
  });
  // A --single-branch clone has no origin/HEAD; FETCH_HEAD is the commit the
  // fetch above just wrote, and is the same one.
  const target = head.status === 0 && head.stdout.trim() ? head.stdout.trim() : "FETCH_HEAD";
  return run(["reset", "--hard", target], dir);
}

// reachable probes a remote without cloning it, so one run can fail once with a
// message about access rather than once per repository.
function reachable(remote) {
  return run(["ls-remote", "--heads", "--", remote]);
}

module.exports = { pull, clone, shallowClone, refresh, reachable };
