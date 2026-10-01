"use strict";
// The activity log: one JSON object per line, appended by work that runs where
// nobody is watching, and read back by the hook that surfaces it.
//
// It exists because the clone sync runs in the background at session start. A
// failure there is silent otherwise - the session carries on with a stale or
// empty set of clones, and the first anybody hears of it is an agent reporting
// that a repository does not exist. The log is how that reaches you instead.
//
// A cursor file records how far the reader has got, so each line is shown
// exactly once however many prompts a session has.

const { appendFileSync, existsSync, mkdirSync, readFileSync, writeFileSync, statSync } = require("node:fs");
const { dirname, join } = require("node:path");

const FILE = "activity.jsonl";
const CURSOR = "activity.cursor";

// Levels, ordered. A threshold keeps the quieter ones out of a hook that fires
// on every prompt.
const LEVELS = ["debug", "info", "warn", "error"];

function rank(level) {
  const at = LEVELS.indexOf(level);
  return at === -1 ? LEVELS.indexOf("info") : at;
}

// MaxBytes caps the log. Past it the file is truncated rather than rotated: it
// is a transcript of recent activity, not an audit trail, and a rotation scheme
// is a second thing to get wrong for data nobody keeps.
const MaxBytes = 1 << 20;

function createLog(root) {
  const file = join(root, FILE);
  const cursor = join(root, CURSOR);

  function write(level, stage, message, fields = {}) {
    try {
      mkdirSync(dirname(file), { recursive: true });
      if (existsSync(file) && statSync(file).size > MaxBytes) {
        writeFileSync(file, "");
        writeFileSync(cursor, "0\n\n");
      }
      appendFileSync(
        file,
        JSON.stringify({ at: new Date().toISOString(), level, stage, message, ...fields }) + "\n",
      );
    } catch {
      // Logging must never be the thing that fails a sync.
    }
  }

  // Lines the reader has not seen, at or above `level`, and the offset to commit
  // once they have been shown. Reading does not advance the cursor: a hook that
  // crashed between reading and printing would otherwise swallow the warning it
  // existed to deliver.
  //
  // The cursor stores the log's first line alongside the offset, because a byte
  // offset on its own cannot tell growth from replacement. A log truncated and
  // then written to again is longer than the stored offset while holding entirely
  // different bytes, so reading from that offset lands mid-line and reports
  // nothing - for the rest of the file's life. A changed first line means a
  // different file, whatever its length.
  function unread(level = "warn") {
    if (!existsSync(file)) return { entries: [], offset: 0 };
    const body = readFileSync(file, "utf8");
    const { offset, signature } = readCursor();
    let from = offset;
    if (from > body.length || signature !== firstLine(body)) from = 0;

    const entries = [];
    for (const line of body.slice(from).split("\n")) {
      if (!line.trim()) continue;
      try {
        const entry = JSON.parse(line);
        if (rank(entry.level) >= rank(level)) entries.push(entry);
      } catch {
        // A half-written final line is normal while a background writer runs.
      }
    }
    return { entries, offset: body.length };
  }

  function readCursor() {
    try {
      const [offset, ...rest] = readFileSync(cursor, "utf8").split("\n");
      return { offset: Number(offset) || 0, signature: rest.join("\n").trimEnd() };
    } catch {
      return { offset: 0, signature: "" };
    }
  }

  function commit(offset) {
    try {
      mkdirSync(dirname(cursor), { recursive: true });
      const body = existsSync(file) ? readFileSync(file, "utf8") : "";
      writeFileSync(cursor, String(offset) + "\n" + firstLine(body) + "\n");
    } catch {}
  }

  return {
    file,
    debug: (stage, message, fields) => write("debug", stage, message, fields),
    info: (stage, message, fields) => write("info", stage, message, fields),
    warn: (stage, message, fields) => write("warn", stage, message, fields),
    error: (stage, message, fields) => write("error", stage, message, fields),
    unread,
    commit,
  };
}

// firstLine identifies a log file cheaply: it holds a timestamp, so two files are
// near-certain to differ, and comparing it costs no hashing.
function firstLine(body) {
  return body.split("\n", 1)[0].slice(0, 200);
}

// render turns entries into the one block a hook shows. Stage-labelled, newest
// last, because that is the order they happened in.
function render(entries) {
  return entries.map((e) => `repomap ${e.stage}: ${e.message}`).join("\n");
}

module.exports = { createLog, render, LEVELS, rank };
