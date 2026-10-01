"use strict";
// The alerting channel: work that ran where nobody was watching, surfaced at the
// next prompt. The thing worth pinning is that each line is shown exactly once -
// a hook that repeats itself on every prompt is a hook people learn to ignore.

const { test } = require("node:test");
const assert = require("node:assert");
const { mkdtempSync, writeFileSync } = require("node:fs");
const { join } = require("node:path");
const { tmpdir } = require("node:os");

const { createLog, render } = require("../lib/log.js");
const { main, MaxLines } = require("../notify-activity.js");

function data() {
  return mkdtempSync(join(tmpdir(), "repomap-notify-"));
}

function hook(root, env = {}) {
  const saved = { ...process.env };
  Object.assign(process.env, env);
  try {
    return main([`--data-dir=${root}`]);
  } finally {
    process.env = saved;
  }
}

test("a healthy sync says nothing", () => {
  const root = data();
  createLog(root).info("sync", "4 up to date");
  // info is below the default threshold: progress is not an alert.
  assert.equal(hook(root), null);
});

test("a failure reaches the next prompt", () => {
  const root = data();
  createLog(root).error("sync", "cannot reach git@example.internal:a/api.git");
  assert.match(hook(root), /cannot reach/);
});

// Shown once, however many prompts a session has.
test("each line is reported exactly once", () => {
  const root = data();
  const log = createLog(root);
  log.warn("sync", "api could not be fetched");

  assert.match(hook(root), /api could not be fetched/);
  assert.equal(hook(root), null);

  log.warn("sync", "web could not be fetched");
  const second = hook(root);
  assert.match(second, /web/);
  assert.ok(!/api/.test(second), "an already-reported line came back");
});

test("the threshold can be lowered for one session", () => {
  const root = data();
  createLog(root).info("sync", "4 up to date");
  assert.match(hook(root, { REPOMAP_NOTIFY_LEVEL: "info" }), /4 up to date/);
});

test("a standing preference on disk is honoured", () => {
  const root = data();
  const { createRegistry } = require("../lib/registry.js");
  createRegistry(root).setNotifyLevel("info");
  createLog(root).info("sync", "4 up to date");
  assert.match(hook(root), /4 up to date/);
});

// A sync that failed across two hundred repositories has already said so in its
// summary; pasting two hundred rows into a prompt teaches people to skip this.
test("a flood is capped and says how much was left out", () => {
  const root = data();
  const log = createLog(root);
  for (let i = 0; i < MaxLines + 7; i++) log.warn("sync", `repo${i} could not be fetched`);

  const text = hook(root);
  assert.equal(text.split("\n").length, MaxLines + 1);
  assert.match(text, /7 earlier line\(s\) not shown/);
});

// A hook that broke a prompt would be worse than a missed notice.
test("a corrupt log does not break the prompt", () => {
  const root = data();
  writeFileSync(join(root, "activity.jsonl"), "not json at all\n{\"level\":\"warn\"");
  assert.doesNotThrow(() => hook(root));
});

test("a missing log is silent", () => {
  assert.equal(hook(data()), null);
});

test("render labels each line with the stage that wrote it", () => {
  assert.equal(
    render([{ stage: "sync", message: "api could not be fetched" }]),
    "repomap sync: api could not be fetched",
  );
});

// The log is a transcript of recent activity, not an audit trail, so it is
// truncated rather than grown without bound - and a cursor left past the end of a
// truncated file must not silence the reader forever.
test("a truncated log does not strand the cursor", () => {
  const root = data();
  const log = createLog(root);
  log.warn("sync", "first");
  hook(root);
  writeFileSync(join(root, "activity.jsonl"), "");
  log.warn("sync", "after truncation");
  assert.match(hook(root), /after truncation/);
});
