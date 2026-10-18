// @ts-check
// Plain-text output helpers. Every number the report prints carries a unit and a source tag.

export const TAG = Object.freeze({ billing: '[billing API]', timing: '[run timing]', estimate: '[estimate]' });

/**
 * @param {number} value
 * @param {number} [decimals]
 * @returns {string}
 */
export function num(value, decimals = 0) {
  if (!Number.isFinite(value)) return 'n/a';
  return value.toLocaleString('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
}

/**
 * @param {number} value
 * @returns {string}
 */
export function minutes(value) {
  return `${num(value, value !== 0 && Math.abs(value) < 10 && !Number.isInteger(value) ? 1 : 0)} min`;
}

/**
 * @param {number} value
 * @returns {string}
 */
export function gigabytes(value) {
  return `${num(value, value < 10 ? 2 : 1)} GB`;
}

/**
 * @param {number} value
 * @returns {string}
 */
export function percent(value) {
  return Number.isFinite(value) ? `${num(value, value < 10 ? 1 : 0)}%` : 'n/a';
}

/**
 * @param {Date} date
 * @returns {string}
 */
export function day(date) {
  return date.toISOString().slice(0, 10);
}

/**
 * Left-aligned plain-text table with two spaces between columns.
 * @param {string[]} headers
 * @param {string[][]} rows
 * @returns {string}
 */
export function table(headers, rows) {
  const widths = headers.map((header, column) =>
    Math.max(header.length, ...rows.map((row) => (row[column] ?? '').length)),
  );
  const render = (/** @type {string[]} */ cells) =>
    cells.map((cell, column) => (cell ?? '').padEnd(widths[column])).join('  ').trimEnd();
  return [render(headers), ...rows.map(render)].join('\n');
}
