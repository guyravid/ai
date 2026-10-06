export type JsonObject = Record<string, unknown>;

export class ValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ValidationError";
  }
}

export function isObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function expectObject(value: unknown, path: string): JsonObject {
  if (!isObject(value)) throw new ValidationError(`${path} must be an object`);
  return value;
}

export function expectString(value: unknown, path: string): string {
  if (typeof value !== "string") throw new ValidationError(`${path} must be a string`);
  return value;
}

export function expectNonEmptyString(value: unknown, path: string): string {
  const text = expectString(value, path);
  if (text.trim() === "") throw new ValidationError(`${path} must not be empty`);
  return text;
}

export function expectNumber(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new ValidationError(`${path} must be a finite number`);
  }
  return value;
}

export function expectBoolean(value: unknown, path: string): boolean {
  if (typeof value !== "boolean") throw new ValidationError(`${path} must be a boolean`);
  return value;
}

export function expectArray(value: unknown, path: string): unknown[] {
  if (!Array.isArray(value)) throw new ValidationError(`${path} must be an array`);
  return value;
}

export function expectStringArray(value: unknown, path: string): string[] {
  return expectArray(value, path).map((entry, index) => expectString(entry, `${path}[${index}]`));
}

export function expectOneOf<T extends string>(value: unknown, allowed: readonly T[], path: string): T {
  const text = expectString(value, path);
  const match = allowed.find((candidate) => candidate === text);
  if (match === undefined) throw new ValidationError(`${path} must be one of: ${allowed.join(", ")}`);
  return match;
}

export function isNullish(value: unknown): value is null | undefined {
  return value === null || value === undefined;
}
