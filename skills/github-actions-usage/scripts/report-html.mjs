#!/usr/bin/env node
// @ts-check
// Render the HTML report from JSON written by usage.mjs / risk.mjs --json.
//
// Usage: node report-html.mjs --usage <usage.json> [--risk <risk.json>] --out <file.html> [--artifact]
//   --artifact  write page content only (no doctype/html/head/body), the form the Artifact tool publishes
import { readFileSync, writeFileSync } from 'node:fs';
import { parseArgs } from 'node:util';
import { renderReport } from './lib/html.mjs';

const { values: args } = parseArgs({
  options: {
    usage: { type: 'string' },
    risk: { type: 'string' },
    out: { type: 'string' },
    artifact: { type: 'boolean' },
  },
});
if (!args.usage || !args.out) {
  console.error('Usage: node report-html.mjs --usage <usage.json> [--risk <risk.json>] --out <file.html> [--artifact]');
  process.exit(2);
}

const quotas = JSON.parse(readFileSync(new URL('./lib/quotas.json', import.meta.url), 'utf8'));
const usage = JSON.parse(readFileSync(args.usage, 'utf8'));
const risk = args.risk ? JSON.parse(readFileSync(args.risk, 'utf8')) : null;
writeFileSync(
  args.out,
  renderReport(usage, risk, { standalone: !args.artifact, sources: quotas.sources, readDate: quotas.readDate }),
);
console.log(`HTML report written to ${args.out}`);
