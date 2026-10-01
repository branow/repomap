#!/usr/bin/env node
"use strict";
// Brings the clones an index points at up to date. Run with no arguments at
// session start in the background, and by name when somebody wants one
// repository now.
//
// Usage: node sync.js [--data-dir=PATH] [--workspace=N] [--repo=N]
//                     [--threshold=HOURS] [--concurrency=N] [--prune] [--quiet]
//
// It reads clone urls out of the catalog the build wrote, so it needs no forge
// credential and no repomap binary: a reader who pulled a published index can
// fetch the code with nothing but git.

const args = require("./lib/args.js");
const clones = require("./lib/clones.js");
const { createLog } = require("./lib/log.js");
const { createRegistry } = require("./lib/registry.js");
const { resolveAll } = require("./roots.js");

async function main(argv) {
  const { flags } = args.parse(argv);
  const registry = createRegistry(flags["data-dir"] || undefined);
  const log = createLog(registry.root);

  const only = args.value(flags, "repo");
  const wanted = args.value(flags, "workspace");
  const threshold = Number(args.value(flags, "threshold", clones.StaleAfterHours));
  const concurrency = Number(args.value(flags, "concurrency", clones.Concurrency));
  // Asking for one repository means asking for it NOW; waiting out a staleness
  // window is what the background run is for.
  const effective = only ? 0 : threshold;

  const targets = clones.workspacesOf(resolveAll(registry).trees).filter(
    (t) => !wanted || t.workspace === wanted,
  );
  if (targets.length === 0) {
    const detail = wanted ? `no workspace named "${wanted}"` : "no index tree on this machine";
    say(flags, `${detail}; nothing to sync`);
    return 0;
  }

  // One run at a time across every session on this machine, so two terminals
  // opening together do not fetch the same repositories into the same
  // directories.
  const release = clones.lock(registry);
  if (!release) {
    say(flags, "another sync is already running");
    return 0;
  }

  let failures = 0;
  try {
    for (const target of targets) {
      const result = await clones.sync(registry, target.path, target.workspace, {
        threshold: effective,
        concurrency,
        only,
        forge: target.forge,
        log,
      });
      if (result.local) {
        say(flags, `${target.workspace}: local checkouts, nothing to fetch`);
        continue;
      }
      if (flags.prune) {
        const removed = clones.prune(registry, target.path, target.workspace);
        if (removed.length > 0) {
          log.info("sync", `${removed.length} retired clone(s) removed: ${removed.join(", ")}`);
        }
      }
      failures += result.failed.length + (result.access ? 1 : 0);
      const parts = [`${result.synced} fetched`];
      if (result.failed.length > 0) parts.push(`${result.failed.length} failed`);
      if (result.skipped.length > 0) parts.push(`${result.skipped.length} with no clone url`);
      if (result.access) parts.push(`${result.access} unreachable`);
      say(flags, `${target.workspace}: ${parts.join(", ")}`);
    }
  } finally {
    release();
  }
  return failures > 0 ? 1 : 0;
}

function say(flags, text) {
  if (!flags.quiet) process.stdout.write(text + "\n");
}

if (require.main === module) {
  main(process.argv.slice(2))
    .then((code) => process.exit(code))
    .catch((err) => {
      if (err instanceof args.UsageError) {
        process.stderr.write(err.message + "\n");
        process.exit(1);
      }
      // A background sync must not print a stack trace into a session start.
      process.stderr.write(`sync failed: ${err.message}\n`);
      process.exit(1);
    });
}

module.exports = { main };
