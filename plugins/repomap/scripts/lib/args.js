"use strict";
// The smallest argument parser that covers this plugin's scripts: --key=value,
// bare --flag, and everything else positional. No dependencies, because a plugin
// that needs an install step before its hook can run is a plugin that breaks
// session start.

function parse(argv) {
  const flags = {};
  const positionals = [];
  for (const arg of argv) {
    if (!arg.startsWith("--")) {
      positionals.push(arg);
      continue;
    }
    const body = arg.slice(2);
    const eq = body.indexOf("=");
    if (eq === -1) flags[body] = true;
    else flags[body.slice(0, eq)] = body.slice(eq + 1);
  }
  return { flags, positionals };
}

// value reads a flag that must carry a string, and reports the `--key=value`
// form when it does not.
//
// Only `--key=value` is parsed, so `--name gh` leaves the flag boolean and the
// value stranded in the positionals. Read naively that silently registered a
// tree under the default name and dropped what the user typed, so a bare flag
// where a value belongs is an error rather than a default.
function value(flags, key, fallback = null) {
  const raw = flags[key];
  if (raw === undefined) return fallback;
  if (raw === true) throw new UsageError(`--${key} needs a value: write --${key}=<value>`);
  return raw;
}

// UsageError is a mistake in what the user typed, which the caller reports as a
// message rather than a stack trace.
class UsageError extends Error {
  constructor(message) {
    super(message);
    this.name = "UsageError";
  }
}

module.exports = { parse, value, UsageError };
