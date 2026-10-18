// @ts-check
// Read-only access to GitHub through the gh CLI. Only GET requests are ever made: no -X, -f or -F
// flags are passed, and the auth token is never read or stored here; gh handles it.
import { execFileSync } from 'node:child_process';

const MAX_BUFFER = 256 * 1024 * 1024;

export class GhError extends Error {
  /**
   * @param {string} message
   * @param {string} stderr
   */
  constructor(message, stderr) {
    super(message);
    this.stderr = stderr;
  }

  get isNotFound() {
    return /\(HTTP 404\)/.test(this.stderr);
  }

  /** @returns {string | null} scope gh says the call needs, if any */
  get missingScope() {
    const match = this.stderr.match(/needs the "([^"]+)" scope/);
    return match ? match[1] : null;
  }
}

/**
 * @param {string[]} args
 * @returns {string}
 */
function runGh(args) {
  try {
    return execFileSync('gh', args, { encoding: 'utf8', maxBuffer: MAX_BUFFER, stdio: ['ignore', 'pipe', 'pipe'] });
  } catch (error) {
    const failure = /** @type {{ stderr?: string, message: string }} */ (error);
    const stderr = String(failure.stderr ?? '');
    throw new GhError(`gh ${args.join(' ')} failed: ${stderr.trim() || failure.message}`, stderr);
  }
}

/**
 * GET a REST path and parse the JSON body.
 * @param {string} path e.g. "/user" or "repos/o/r/actions/runs?per_page=100"
 * @returns {any}
 */
export function apiGet(path) {
  return JSON.parse(runGh(['api', path]));
}

/**
 * GET a paginated REST path and return every element of `arrayField` across all pages.
 * Each element is emitted with `tojson` so one line is one object, whatever gh's output formatting.
 * @param {string} path
 * @param {string} arrayField e.g. "workflow_runs"
 * @returns {any[]}
 */
export function apiGetAll(path, arrayField) {
  const output = runGh(['api', '--paginate', path, '--jq', `.${arrayField}[] | tojson`]);
  return output
    .split('\n')
    .filter((line) => line.trim() !== '')
    .map((line) => JSON.parse(line));
}

/**
 * Run a read-only gh subcommand that prints JSON (e.g. `repo list ... --json ...`).
 * @param {string[]} args
 * @returns {any}
 */
export function ghJson(args) {
  return JSON.parse(runGh(args));
}

/**
 * OAuth scopes of the token gh uses, from the X-OAuth-Scopes response header.
 * Returns null when the header is absent (e.g. fine-grained token), meaning "unknown".
 * @returns {string[] | null}
 */
export function tokenScopes() {
  const output = runGh(['api', '-i', 'user']);
  const header = output.split('\n').find((line) => /^x-oauth-scopes:/i.test(line));
  if (header === undefined) return null;
  return header
    .slice(header.indexOf(':') + 1)
    .split(',')
    .map((scope) => scope.trim())
    .filter(Boolean);
}

/**
 * Notice text for a missing scope, with the exact command the user runs themselves.
 * @param {string} scope
 * @param {string} consequence
 * @returns {string}
 */
export function missingScopeNotice(scope, consequence) {
  return [
    `NOTICE: the gh token is missing the "${scope}" scope. ${consequence}`,
    `        To add it, run:  gh auth refresh -h github.com -s ${scope}`,
  ].join('\n');
}
