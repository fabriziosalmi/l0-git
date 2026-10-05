// Freshness: how current a project's findings are. Pure functions with no
// `vscode` import, so they can be tested by node alone.
//
// Findings only change when a project is re-checked, so every count the
// extension shows is as old as that check. `lgit stats` reports it
// (last_checked_at in milliseconds, project_exists); this turns it into text.

export interface FreshnessInput {
  // Milliseconds since the epoch of the last FULL check; 0 = none on record.
  // Absent means "unknown" (an older binary, or a store that could not say),
  // which is NOT the same as 0.
  last_checked_at?: unknown;
  // False when the directory could not be found. Absent = could not be examined.
  project_exists?: unknown;
}

export type FreshnessLevel = "fresh" | "stale" | "never" | "missing";

export interface Freshness {
  level: FreshnessLevel;
  text: string;
}

export const DEFAULT_STALE_AFTER_DAYS = 7;
const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

function plural(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? "" : "s"} ago`;
}

// ageText renders an age in milliseconds. A negative age (a clock that moved
// back, or another machine's clock) reads as "just now" rather than as a lie.
export function ageText(ageMs: number): string {
  if (!Number.isFinite(ageMs) || ageMs < MINUTE) return "just now";
  if (ageMs < HOUR) return plural(Math.floor(ageMs / MINUTE), "minute");
  if (ageMs < DAY) return plural(Math.floor(ageMs / HOUR), "hour");
  return plural(Math.floor(ageMs / DAY), "day");
}

// normalizeStaleAfterDays keeps a bad setting (0, negative, NaN, a string) from
// disabling or inverting the warning: it falls back to the default.
export function normalizeStaleAfterDays(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) && v >= 1 ? v : DEFAULT_STALE_AFTER_DAYS;
}

// describeFreshness returns what to say about a project's findings, or
// undefined when there is nothing honest to say (the fields are absent).
//
//   - project_exists === false  -> "directory not found". Not "deleted": an
//     unmounted volume looks exactly the same from here.
//   - last_checked_at === 0     -> "never checked" (present, and zero).
//   - last_checked_at absent    -> undefined. An older binary does not know;
//     "never checked" would be a claim it never made.
//   - otherwise                 -> "checked 3 days ago", level "stale" past the
//     threshold.
export function describeFreshness(
  s: FreshnessInput,
  nowMs: number,
  staleAfterDays: unknown = DEFAULT_STALE_AFTER_DAYS,
): Freshness | undefined {
  if (s.project_exists === false) {
    return { level: "missing", text: "directory not found" };
  }
  const at = s.last_checked_at;
  if (typeof at !== "number" || !Number.isFinite(at) || at < 0) return undefined;
  if (at === 0) return { level: "never", text: "never checked" };
  const age = nowMs - at;
  const stale = age > normalizeStaleAfterDays(staleAfterDays) * DAY;
  return { level: stale ? "stale" : "fresh", text: `checked ${ageText(age)}` };
}

// oldestFreshness picks the entry that should speak for several projects (a
// multi-root workspace): the one that is worst. Order: missing, never, stale,
// fresh; the first of equals wins. Unknown (undefined) entries are ignored, never counted
// as fresh or as never.
export function oldestFreshness(items: Array<Freshness | undefined>): Freshness | undefined {
  const rank: Record<FreshnessLevel, number> = { missing: 3, never: 2, stale: 1, fresh: 0 };
  let worst: Freshness | undefined;
  for (const f of items) {
    if (f && (!worst || rank[f.level] > rank[worst.level])) worst = f;
  }
  return worst;
}

// pruneHint is the one-line pointer shown when the shared store holds projects
// whose directory is gone. Nothing when the count is absent or zero.
export function pruneHint(projectsMissing: unknown): string | undefined {
  if (typeof projectsMissing !== "number" || !Number.isInteger(projectsMissing) || projectsMissing < 1) {
    return undefined;
  }
  const [noun, verb] = projectsMissing === 1 ? ["project", "exists"] : ["projects", "exist"];
  return `${projectsMissing} ${noun} in the shared store no longer ${verb} on disk. Run "lgit prune" to see what can be cleaned up.`;
}
