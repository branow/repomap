#!/usr/bin/env node
"use strict";
// UserPromptSubmit hook. Surfaces what the background clone sync has logged
// since the last prompt, through the hook's systemMessage, and exits.
//
// This is the alerting channel, and it is deliberately only that. The sync runs
// at session start where nobody is watching: without this, a dead SSH key or a
// dropped VPN means the session quietly carries on with no clones, and the first
// anybody hears of it is an agent reporting that a file does not exist.
//
// It fires on every prompt, so the default threshold is `warn`: problems reach
// you, routine progress does not. REPOMAP_NOTIFY_LEVEL overrides it for a
// session, and `notify.level` in the plugin's own settings for a machine.
//
// Flag (from hooks.json): --data-dir=PATH - the plugin's data directory.

const args = require("./lib/args.js");
const { createLog, render, LEVELS } = require("./lib/log.js");
const { createRegistry } = require("./lib/registry.js");

// MaxLines caps one message. A sync that failed across two hundred repositories
// has already said so in its summary line; pasting two hundred rows into a
// prompt is how somebody learns to ignore this channel.
const MaxLines = 12;

function main(argv) {
  const { flags } = args.parse(argv);
  const registry = createRegistry(flags["data-dir"] || undefined);
  const log = createLog(registry.root);

  const { entries, offset } = log.unread(level(registry));
  // The cursor advances whether or not there was anything to say, so a quiet
  // sync does not leave its info lines to be re-read at the next threshold
  // change.
  log.commit(offset);
  if (entries.length === 0) return null;

  const shown = entries.slice(-MaxLines);
  const text = render(shown);
  if (entries.length > shown.length) {
    return `${text}\n(${entries.length - shown.length} earlier line(s) not shown)`;
  }
  return text;
}

// level resolves the threshold, most dynamic source first: a per-session
// environment variable, then a standing preference on disk, then warn.
function level(registry) {
  const fromEnv = process.env.REPOMAP_NOTIFY_LEVEL;
  if (LEVELS.includes(fromEnv)) return fromEnv;
  const stored = registry.notifyLevel();
  if (LEVELS.includes(stored)) return stored;
  return "warn";
}

let text = null;
try {
  text = main(process.argv.slice(2));
} catch {
  // A hook must never break a prompt, and a missed notice costs nothing.
}
process.stdout.write(JSON.stringify(text ? { systemMessage: text } : {}) + "\n");

module.exports = { main, MaxLines };
