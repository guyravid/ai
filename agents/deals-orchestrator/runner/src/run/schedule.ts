import type { RunConfig, TrackedRole } from "./run-config.ts";

const MS_PER_MINUTE = 60_000;
const MS_PER_HOUR = 60 * MS_PER_MINUTE;
const MS_PER_DAY = 24 * MS_PER_HOUR;

function parseTime(timestamp: string | undefined): number | null {
  if (timestamp === undefined) return null;
  const time = Date.parse(timestamp);
  return Number.isNaN(time) ? null : time;
}

export function isItemDue(now: Date, lastRunAt: string | undefined, role: TrackedRole, config: RunConfig): boolean {
  const last = parseTime(lastRunAt);
  if (last === null) return true;
  const cadenceMs = config.cadenceHours[role] * MS_PER_HOUR;
  return now.getTime() >= last + cadenceMs - config.dueToleranceMinutes * MS_PER_MINUTE;
}

export function isSweepDue(
  now: Date,
  lastSweepAt: string | undefined,
  role: TrackedRole,
  config: RunConfig,
  forceSweep: boolean,
): boolean {
  if (forceSweep) return true;
  const last = parseTime(lastSweepAt);
  if (last === null) return true;
  return now.getTime() >= last + config.sweepDays[role] * MS_PER_DAY;
}
