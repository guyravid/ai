export class StoreError extends Error {
  readonly code: string;
  readonly retriable: boolean;

  constructor(code: string, message: string, retriable = false) {
    super(message);
    this.name = "StoreError";
    this.code = code;
    this.retriable = retriable;
  }
}
