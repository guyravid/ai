export interface Clock {
  now: () => number;
  sleep: (milliseconds: number) => Promise<void>;
}

export const systemClock: Clock = {
  now: () => Date.now(),
  sleep: (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)),
};

export const DEFAULT_MIN_INTERVAL_MS = 2000;

export class HostRateLimiter {
  private readonly minIntervalMs: number;
  private readonly clock: Clock;
  private readonly nextAllowedAt = new Map<string, number>();

  constructor(options: { minIntervalMs?: number; clock?: Clock } = {}) {
    this.minIntervalMs = options.minIntervalMs ?? DEFAULT_MIN_INTERVAL_MS;
    this.clock = options.clock ?? systemClock;
  }

  async wait(host: string): Promise<void> {
    const now = this.clock.now();
    const allowedAt = this.nextAllowedAt.get(host) ?? now;
    const startAt = Math.max(now, allowedAt);
    this.nextAllowedAt.set(host, startAt + this.minIntervalMs);
    if (startAt > now) await this.clock.sleep(startAt - now);
  }
}
