import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";

export interface RunLock {
  release(): Promise<void>;
}

export interface LockOptions {
  pid: number;
  isAlive: (pid: number) => boolean;
}

export function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return typeof error === "object" && error !== null && "code" in error && error.code === "EPERM";
  }
}

function isFileExists(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "EEXIST";
}

export async function acquireLock(directory: string, options: LockOptions): Promise<RunLock | null> {
  const path = join(directory, "run.lock");
  await mkdir(directory, { recursive: true });
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      await writeFile(path, String(options.pid), { flag: "wx" });
      return { release: () => rm(path, { force: true }) };
    } catch (error) {
      if (!isFileExists(error)) throw error;
      const holder = Number((await readFile(path, "utf8").catch(() => "")).trim());
      if (Number.isInteger(holder) && holder > 0 && options.isAlive(holder)) return null;
      await rm(path, { force: true });
    }
  }
  return null;
}

export async function withLock<T>(
  directory: string,
  options: LockOptions,
  action: () => Promise<T>,
): Promise<T | { skipped: "locked" }> {
  const lock = await acquireLock(directory, options);
  if (lock === null) return { skipped: "locked" };
  try {
    return await action();
  } finally {
    await lock.release();
  }
}
