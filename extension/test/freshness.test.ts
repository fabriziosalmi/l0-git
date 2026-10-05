import { test } from "node:test";
import assert from "node:assert/strict";
import {
  ageText,
  describeFreshness,
  normalizeStaleAfterDays,
  oldestFreshness,
  pruneHint,
} from "../src/freshness";

const NOW = Date.UTC(2026, 9, 5, 12, 0, 0);
const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

test("ageText: unit boundaries and plurals", () => {
  assert.equal(ageText(0), "just now");
  assert.equal(ageText(59_999), "just now");
  assert.equal(ageText(MIN), "1 minute ago");
  assert.equal(ageText(2 * MIN), "2 minutes ago");
  assert.equal(ageText(HOUR - 1), "59 minutes ago");
  assert.equal(ageText(HOUR), "1 hour ago");
  assert.equal(ageText(23 * HOUR + 59 * MIN), "23 hours ago");
  assert.equal(ageText(DAY), "1 day ago");
  assert.equal(ageText(3 * DAY + 5 * HOUR), "3 days ago");
});

test("ageText: a negative or non-finite age is 'just now', never a lie", () => {
  assert.equal(ageText(-5 * DAY), "just now");
  assert.equal(ageText(Number.NaN), "just now");
  assert.equal(ageText(Number.POSITIVE_INFINITY), "just now");
});

test("describeFreshness: a recent check is fresh", () => {
  assert.deepEqual(describeFreshness({ last_checked_at: NOW - 3 * HOUR }, NOW), {
    level: "fresh",
    text: "checked 3 hours ago",
  });
});

test("describeFreshness: stale only past the threshold, boundary inclusive of exactly N days", () => {
  assert.equal(describeFreshness({ last_checked_at: NOW - 7 * DAY }, NOW, 7)?.level, "fresh");
  assert.equal(describeFreshness({ last_checked_at: NOW - 7 * DAY - 1 }, NOW, 7)?.level, "stale");
  assert.equal(describeFreshness({ last_checked_at: NOW - 3 * DAY }, NOW, 2)?.level, "stale");
  assert.equal(describeFreshness({ last_checked_at: NOW - 3 * DAY }, NOW, 14)?.level, "fresh");
});

test("describeFreshness: 0 is present and reads 'never checked'", () => {
  assert.deepEqual(describeFreshness({ last_checked_at: 0 }, NOW), { level: "never", text: "never checked" });
});

test("describeFreshness: an ABSENT field is unknown, not 'never' (older binary)", () => {
  assert.equal(describeFreshness({}, NOW), undefined);
  assert.equal(describeFreshness({ project_exists: true }, NOW), undefined);
});

test("describeFreshness: junk in the field is unknown, not a crash or a claim", () => {
  for (const v of ["", "12", null, -1, Number.NaN, {}, [], true]) {
    assert.equal(describeFreshness({ last_checked_at: v }, NOW), undefined, JSON.stringify(v));
  }
});

test("describeFreshness: a missing directory says 'directory not found', whatever the age", () => {
  const want = { level: "missing", text: "directory not found" };
  assert.deepEqual(describeFreshness({ project_exists: false }, NOW), want);
  assert.deepEqual(describeFreshness({ project_exists: false, last_checked_at: NOW - HOUR }, NOW), want);
  assert.deepEqual(describeFreshness({ project_exists: false, last_checked_at: 0 }, NOW), want);
});

test("describeFreshness: project_exists true or absent does not hide the age", () => {
  assert.equal(describeFreshness({ project_exists: true, last_checked_at: NOW - HOUR }, NOW)?.text, "checked 1 hour ago");
});

test("describeFreshness: a check from the future is fresh and 'just now'", () => {
  assert.deepEqual(describeFreshness({ last_checked_at: NOW + 2 * DAY }, NOW), { level: "fresh", text: "checked just now" });
});

test("normalizeStaleAfterDays: bad settings fall back to 7", () => {
  for (const v of [0, -3, 0.5, Number.NaN, "7", null, undefined, Number.POSITIVE_INFINITY]) {
    assert.equal(normalizeStaleAfterDays(v), 7, String(v));
  }
  assert.equal(normalizeStaleAfterDays(1), 1);
  assert.equal(normalizeStaleAfterDays(30), 30);
});

test("describeFreshness: a bad threshold cannot turn the warning off", () => {
  assert.equal(describeFreshness({ last_checked_at: NOW - 30 * DAY }, NOW, 0)?.level, "stale");
  assert.equal(describeFreshness({ last_checked_at: NOW - 30 * DAY }, NOW, Number.NaN)?.level, "stale");
});

test("oldestFreshness: worst wins; unknown is ignored, not counted as fresh", () => {
  const fresh = describeFreshness({ last_checked_at: NOW - HOUR }, NOW);
  const stale = describeFreshness({ last_checked_at: NOW - 30 * DAY }, NOW);
  const never = describeFreshness({ last_checked_at: 0 }, NOW);
  const missing = describeFreshness({ project_exists: false }, NOW);
  assert.equal(oldestFreshness([fresh, stale])?.level, "stale");
  assert.equal(oldestFreshness([stale, never, fresh])?.level, "never");
  assert.equal(oldestFreshness([never, missing, stale])?.level, "missing");
  assert.equal(oldestFreshness([undefined, fresh])?.level, "fresh");
  assert.equal(oldestFreshness([undefined, undefined]), undefined);
  assert.equal(oldestFreshness([]), undefined);
});

test("pruneHint: only for a positive integer count", () => {
  assert.equal(pruneHint(undefined), undefined);
  assert.equal(pruneHint(0), undefined);
  assert.equal(pruneHint(-2), undefined);
  assert.equal(pruneHint(1.5), undefined);
  assert.equal(pruneHint("3"), undefined);
  assert.match(pruneHint(1) ?? "", /^1 project in the shared store no longer exists on disk/);
  assert.match(pruneHint(102) ?? "", /^102 projects in the shared store no longer exist on disk/);
  assert.match(pruneHint(102) ?? "", /lgit prune/);
});
