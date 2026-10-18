// @ts-check
// Renders the usage report (and optionally one repo's risk review) as a self-contained HTML page:
// inline CSS, inline SVG charts, a few lines of inline JS for the chart tooltip, no external requests.
// Pure: takes the JSON written by usage.mjs / risk.mjs --json and returns a string.

const OS_SERIES = Object.freeze({ Linux: 'var(--series-1)', macOS: 'var(--series-2)', Windows: 'var(--series-3)' });
const STATUS_LABEL = Object.freeze({ fine: 'Fine', watch: 'Watch', 'at risk': 'At risk' });
const STATUS_CLASS = Object.freeze({ fine: 'fine', watch: 'watch', 'at risk': 'risk' });
const STATUS_RANK = Object.freeze({ fine: 0, watch: 1, 'at risk': 2 });

/**
 * @param {unknown} value
 * @returns {string}
 */
export function esc(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/**
 * @param {number} value
 * @param {number} [decimals]
 * @returns {string}
 */
function num(value, decimals) {
  if (!Number.isFinite(value)) return 'n/a';
  const places = decimals ?? (Math.abs(value) < 10 && !Number.isInteger(value) ? 1 : 0);
  return value.toLocaleString('en-US', { minimumFractionDigits: places, maximumFractionDigits: places });
}

/**
 * A number with its unit and source tag. Every figure on the page goes through this.
 * @param {number} value
 * @param {string} unit
 * @param {string} tag e.g. "[billing API]"
 * @param {number} [decimals]
 * @returns {string}
 */
export function fig(value, unit, tag, decimals) {
  const places = decimals ?? (unit === 'GB' ? (value < 10 ? 2 : 1) : undefined);
  return `<span class="fig">${esc(num(value, places))}${unit ? `&nbsp;${esc(unit)}` : ''} <span class="src">${esc(tag)}</span></span>`;
}

/**
 * @param {number} value
 * @returns {string}
 */
function pct(value) {
  return Number.isFinite(value) ? `${num(value, value < 10 ? 1 : 0)}%` : 'n/a';
}

/**
 * @param {string} status
 * @returns {string}
 */
function statusPill(status) {
  const key = /** @type {keyof typeof STATUS_CLASS} */ (status);
  const icons = {
    fine: '<path d="M3.5 8.5l3 3 6-7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
    watch: '<path d="M8 2.5l6 11H2z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"/><path d="M8 7v3" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>',
    'at risk': '<circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="M8 4.8v4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/><circle cx="8" cy="11.3" r="1" fill="currentColor"/>',
  };
  return `<span class="pill pill-${STATUS_CLASS[key]}"><svg viewBox="0 0 16 16" aria-hidden="true">${icons[key]}</svg>${esc(STATUS_LABEL[key])}</span>`;
}

/**
 * Positions (as % of the meter width) for a usage meter: the scale runs to 100% of the allowance, or
 * further when the projection overshoots (capped at 150%) so the projection marker stays visible.
 * @param {number} usedPct
 * @param {number} projectedPct
 * @param {{ watchUsedPct: number, atRiskUsedPct: number }} thresholds
 * @returns {{ scaleMax: number, used: number, projected: number, watch: number, risk: number, limit: number }}
 */
export function meterPositions(usedPct, projectedPct, thresholds) {
  const finiteProjected = Number.isFinite(projectedPct) ? projectedPct : 150;
  const scaleMax = Math.min(150, Math.max(100, finiteProjected * 1.05, Number.isFinite(usedPct) ? usedPct : 0));
  const at = (/** @type {number} */ value) => Math.min(100, Math.max(0, (value / scaleMax) * 100));
  return {
    scaleMax,
    used: at(Number.isFinite(usedPct) ? usedPct : scaleMax),
    projected: at(finiteProjected),
    watch: at(thresholds.watchUsedPct),
    risk: at(thresholds.atRiskUsedPct),
    limit: at(100),
  };
}

/**
 * @param {any} limit
 * @param {any} thresholds
 * @returns {string}
 */
function limitCard(limit, thresholds) {
  const positions = meterPositions(limit.usedPct, limit.projectedPct, thresholds);
  const statusClass = STATUS_CLASS[/** @type {keyof typeof STATUS_CLASS} */ (limit.status)];
  return `<article class="limit">
  <header><h3>${esc(limit.label)}</h3>${statusPill(limit.status)}</header>
  <p class="limit-used">${fig(limit.used, limit.unit, limit.usedTag)}</p>
  <p class="limit-of">of ${fig(limit.included, limit.unit, '[docs]', limit.unit === 'GB' ? 2 : 0)} included${limit.includedNote ? ` <span class="muted">(${esc(limit.includedNote)})</span>` : ''} &middot; <strong>${esc(pct(limit.usedPct))}</strong> used</p>
  <div class="meter" role="img" aria-label="${esc(`${limit.label}: ${pct(limit.usedPct)} used, projected ${pct(limit.projectedPct)} at cycle end`)}">
    <div class="meter-fill meter-${statusClass}" style="width:${positions.used.toFixed(2)}%"></div>
    <span class="tick tick-watch" style="left:${positions.watch.toFixed(2)}%" title="watch at ${esc(thresholds.watchUsedPct)}%"></span>
    <span class="tick tick-risk" style="left:${positions.risk.toFixed(2)}%" title="at risk at ${esc(thresholds.atRiskUsedPct)}%"></span>
    ${positions.scaleMax > 100 ? `<span class="tick tick-limit" style="left:${positions.limit.toFixed(2)}%" title="100% of the allowance"></span>` : ''}
    <span class="proj" style="left:${positions.projected.toFixed(2)}%" title="projected at cycle end"></span>
  </div>
  <div class="meter-scale"><span>0%</span><span style="left:${positions.watch.toFixed(2)}%">${esc(thresholds.watchUsedPct)}</span><span style="left:${positions.risk.toFixed(2)}%">${esc(thresholds.atRiskUsedPct)}</span>${positions.scaleMax > 100 ? `<span style="left:${positions.limit.toFixed(2)}%">100</span><span class="end">${esc(num(positions.scaleMax, 0))}%</span>` : ''}</div>
  <p class="limit-proj"><span class="proj-key" aria-hidden="true"></span>Projected at cycle end: ${fig(limit.projected, limit.unit, limit.projectedTag)} (${esc(pct(limit.projectedPct))})</p>
</article>`;
}

/**
 * "Nice" upper bound for an axis: 1, 2, 2.5 or 5 times a power of ten, at or above `value`.
 * @param {number} value
 * @returns {number}
 */
export function niceMax(value) {
  if (!(value > 0)) return 1;
  const power = 10 ** Math.floor(Math.log10(value));
  for (const step of [1, 2, 2.5, 5, 10]) if (step * power >= value) return step * power;
  return 10 * power;
}

/**
 * Cumulative cost-weighted minutes chart: used so far (solid, area), projection to cycle end (dashed),
 * included allowance (rule). One y-scale.
 * @param {any} usage
 * @returns {string}
 */
function cumulativeChart(usage) {
  const width = 720;
  const height = 260;
  const pad = { left: 58, right: 20, top: 18, bottom: 30 };
  const plotWidth = width - pad.left - pad.right;
  const plotHeight = height - pad.top - pad.bottom;
  const { daysInCycle, daysElapsed } = usage.cycle;
  const start = Date.parse(usage.cycle.start);
  const weightedLimit = usage.limits.find((/** @type {any} */ limit) => limit.key === 'minutes-weighted');
  const included = weightedLimit.included;
  const total = weightedLimit.used;
  const projected = weightedLimit.projected;
  const yMax = niceMax(Math.max(included, projected, total) * 1.04);
  const x = (/** @type {number} */ day) => pad.left + (Math.min(day, daysInCycle) / daysInCycle) * plotWidth;
  const y = (/** @type {number} */ value) => pad.top + plotHeight - (value / yMax) * plotHeight;

  /** @type {Map<number, { raw: number, weighted: number }>} */
  const byDay = new Map();
  for (const entry of usage.daily) {
    const index = Math.floor((Date.parse(`${entry.date}T00:00:00Z`) - start) / 86_400_000);
    if (index >= 0 && index < daysInCycle) byDay.set(index, entry);
  }
  const lastDay = Math.min(Math.ceil(daysElapsed), daysInCycle);
  const points = [[0, 0]];
  const rows = [];
  let cumulative = 0;
  let cumulativeRaw = 0;
  for (let day = 0; day < lastDay; day++) {
    const entry = byDay.get(day) ?? { raw: 0, weighted: 0 };
    cumulative += entry.weighted;
    cumulativeRaw += entry.raw;
    points.push([Math.min(day + 1, daysElapsed), cumulative]);
    rows.push({ day, date: new Date(start + day * 86_400_000).toISOString().slice(0, 10), ...entry, cumulative, cumulativeRaw });
  }
  const linePath = points.map(([day, value], index) => `${index === 0 ? 'M' : 'L'}${x(day).toFixed(1)},${y(value).toFixed(1)}`).join(' ');
  const end = points[points.length - 1];
  const areaPath = `${linePath} L${x(end[0]).toFixed(1)},${y(0).toFixed(1)} L${x(0).toFixed(1)},${y(0).toFixed(1)} Z`;

  const yStep = niceMax(yMax / 5);
  const yTicks = Array.from({ length: Math.floor(yMax / yStep + 1e-9) + 1 }, (_, index) => index * yStep);
  const xTicks = [0, 7, 14, 21, 28].filter((day) => day < daysInCycle);
  const monthDay = (/** @type {number} */ day) =>
    new Date(start + day * 86_400_000).toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
  const includedLabelY = y(included) - 6 < pad.top + 10 ? y(included) + 14 : y(included) - 6;

  const svg = `<svg class="chart" viewBox="0 0 ${width} ${height}" role="img" aria-labelledby="cum-title cum-desc">
  <title id="cum-title">Cumulative cost-weighted minutes this cycle</title>
  <desc id="cum-desc">${esc(`${num(total)} min used by day ${num(daysElapsed, 1)} of ${daysInCycle}; projected ${num(projected)} min at cycle end against ${num(included)} min included.`)}</desc>
  ${yTicks.map((value) => `<line class="grid" x1="${pad.left}" x2="${width - pad.right}" y1="${y(value).toFixed(1)}" y2="${y(value).toFixed(1)}"/><text class="axis-label" x="${pad.left - 8}" y="${(y(value) + 4).toFixed(1)}" text-anchor="end">${esc(num(value, 0))}</text>`).join('\n  ')}
  ${xTicks.map((day) => `<text class="axis-label" x="${x(day).toFixed(1)}" y="${height - 10}" text-anchor="${day === 0 ? 'start' : 'middle'}">${esc(monthDay(day))}</text>`).join('\n  ')}
  <line class="baseline" x1="${pad.left}" x2="${width - pad.right}" y1="${y(0).toFixed(1)}" y2="${y(0).toFixed(1)}"/>
  <line class="included" x1="${pad.left}" x2="${width - pad.right}" y1="${y(included).toFixed(1)}" y2="${y(included).toFixed(1)}"/>
  <text class="included-label" x="${width - pad.right}" y="${includedLabelY.toFixed(1)}" text-anchor="end">Included ${esc(num(included))} min</text>
  <line class="today" x1="${x(daysElapsed).toFixed(1)}" x2="${x(daysElapsed).toFixed(1)}" y1="${pad.top}" y2="${y(0).toFixed(1)}"/>
  <text class="axis-label" x="${(x(daysElapsed) + 4).toFixed(1)}" y="${pad.top + 10}">today</text>
  <path class="area" d="${areaPath}"/>
  <path class="line" d="${linePath}"/>
  <path class="projection" d="M${x(end[0]).toFixed(1)},${y(end[1]).toFixed(1)} L${x(daysInCycle).toFixed(1)},${y(projected).toFixed(1)}"/>
  <circle class="endpoint" cx="${x(end[0]).toFixed(1)}" cy="${y(end[1]).toFixed(1)}" r="4.5"/>
  <circle class="proj-end" cx="${x(daysInCycle).toFixed(1)}" cy="${y(projected).toFixed(1)}" r="4"/>
  <text class="value-label" x="${(x(daysInCycle) - 6).toFixed(1)}" y="${(y(projected) - 9).toFixed(1)}" text-anchor="end">${esc(num(projected))} min projected</text>
  <line class="crosshair" x1="0" x2="0" y1="${pad.top}" y2="${y(0).toFixed(1)}" visibility="hidden"/>
  <rect class="hit" x="${pad.left}" y="${pad.top}" width="${plotWidth}" height="${plotHeight}" fill="transparent"/>
</svg>`;

  const tableRows = rows
    .filter((row) => row.raw > 0)
    .map((row) => `<tr><td>${esc(row.date)}</td><td>${fig(row.raw, 'min', usage.dailySource)}</td><td>${fig(row.weighted, 'min', '[estimate]')}</td><td>${fig(row.cumulative, 'min', '[estimate]')}</td></tr>`)
    .join('');
  const chartData = { start, daysInCycle, daysElapsed, left: pad.left, plotWidth, width, rows, source: usage.dailySource };
  return `<figure class="chart-wrap">
  <div class="chart-scroll" data-chart='${esc(JSON.stringify(chartData))}'>${svg}<div class="tooltip" role="status" hidden></div></div>
  <figcaption>Solid line: cost-weighted minutes used so far ${'<span class="src">[estimate]</span>'}, built from daily ${esc(usage.dailySource)} minutes. Dashed: projection at the average daily rate since ${esc(usage.cycle.start.slice(0, 10))}. Rule: minutes included in the ${esc(usage.plan)} plan.</figcaption>
  <details><summary>Daily minutes table</summary><div class="table-scroll"><table><thead><tr><th>Day (UTC)</th><th>Raw</th><th>Cost-weighted</th><th>Cumulative</th></tr></thead><tbody>${tableRows || '<tr><td colspan="4">No usage yet this cycle.</td></tr>'}</tbody></table></div></details>
</figure>`;
}

/**
 * Two stacked bars (raw minutes, cost-weighted minutes) split by runner OS, so the jump in macOS's
 * share is visible at a glance.
 * @param {any} usage
 * @returns {string}
 */
function osChart(usage) {
  const entries = /** @type {any[]} */ (usage.byOs);
  if (entries.length === 0) return '<p class="muted">No minutes used this cycle.</p>';
  const order = ['Linux', 'macOS', 'Windows'];
  const sorted = [...entries].sort((a, b) => (order.indexOf(a.os) + 99) % 99 - (order.indexOf(b.os) + 99) % 99);
  const totals = { raw: sorted.reduce((sum, entry) => sum + entry.raw, 0), weighted: sorted.reduce((sum, entry) => sum + entry.weighted, 0) };
  const bar = (/** @type {'raw' | 'weighted'} */ key, /** @type {string} */ label, /** @type {string} */ tag) => {
    const segments = sorted
      .filter((entry) => entry[key] > 0)
      .map((entry) => {
        const share = totals[key] > 0 ? (entry[key] / totals[key]) * 100 : 0;
        const color = OS_SERIES[/** @type {keyof typeof OS_SERIES} */ (entry.os)] ?? 'var(--muted)';
        return `<span class="seg" style="flex-basis:${share.toFixed(2)}%;background:${color}" title="${esc(`${entry.os}: ${num(entry[key])} min, ${pct(share)}`)}">${share >= 14 ? `<span class="seg-label">${esc(entry.os)} ${esc(pct(share))}</span>` : ''}</span>`;
      })
      .join('');
    return `<div class="stack-row"><div class="stack-head"><span>${esc(label)}</span>${fig(totals[key], 'min', tag)}</div><div class="stack">${segments}</div></div>`;
  };
  const legend = sorted
    .map((entry) => `<li><span class="swatch" style="background:${OS_SERIES[/** @type {keyof typeof OS_SERIES} */ (entry.os)] ?? 'var(--muted)'}"></span>${esc(entry.os)}</li>`)
    .join('');
  const rows = sorted
    .map(
      (entry) => `<tr><td><span class="swatch" style="background:${OS_SERIES[/** @type {keyof typeof OS_SERIES} */ (entry.os)] ?? 'var(--muted)'}"></span>${esc(entry.os)}</td>
<td>${fig(entry.raw, 'min', usage.minutesSource)}</td><td>${fig(entry.multiplier, 'x', '[estimate]', 2)}</td><td>${fig(entry.weighted, 'min', '[estimate]')}</td>
<td>${entry.hasDollars ? `${fig(entry.gross, 'USD', '[billing API]', 3)} / ${fig(entry.covered, 'USD', '[billing API]', 3)}` : '<span class="muted">n/a without billing API</span>'}</td></tr>`,
    )
    .join('');
  return `<div class="stacks">${bar('raw', 'Raw minutes', usage.minutesSource)}${bar('weighted', 'Cost-weighted minutes', '[estimate]')}</div>
<ul class="legend">${legend}</ul>
<div class="table-scroll"><table><thead><tr><th>Runner OS</th><th>Raw</th><th>Multiplier</th><th>Cost-weighted</th><th>Gross / covered by included</th></tr></thead><tbody>${rows}</tbody></table></div>`;
}

/**
 * @param {any} usage
 * @returns {string}
 */
function breakdown(usage) {
  const repos = /** @type {any[]} */ (usage.privateRepos);
  const privateHtml = repos.length
    ? repos
        .map((repo) => {
          const workflows = repo.workflows
            .map(
              (/** @type {any} */ workflow) => `<tr class="wf"><td>${esc(workflow.name)} <code>${esc(workflow.path)}</code></td><td>${fig(workflow.raw, 'min', '[run timing]')}</td><td>${fig(workflow.weighted, 'min', '[estimate]')}</td><td></td></tr>
${workflow.jobs.map((/** @type {any} */ job) => `<tr class="job"><td>${esc(job.name)}</td><td>${fig(job.raw, 'min', '[run timing]')}</td><td>${fig(job.weighted, 'min', '[estimate]')}</td><td>${fig(job.attempts, 'job runs', '[run timing]')}</td></tr>`).join('\n')}`,
            )
            .join('\n');
          const mismatch =
            usage.minutesSource === '[billing API]' && repo.timingRaw !== repo.raw
              ? `<p class="muted">Run timing sums to ${fig(repo.timingRaw, 'min', '[run timing]')}; the billing API reports ${fig(repo.raw, 'min', '[billing API]')}.</p>`
              : '';
          return `<details class="repo" open><summary><span class="repo-name">${esc(repo.repo)}</span><span class="repo-figs">${fig(repo.raw, 'min raw', usage.minutesSource)} &middot; ${fig(repo.weighted, 'min cost-weighted', '[estimate]')} &middot; ${fig(repo.runs, 'runs', '[run timing]')}</span></summary>
<div class="table-scroll"><table><thead><tr><th>Workflow / job</th><th>Raw</th><th>Cost-weighted</th><th>Job runs</th></tr></thead><tbody>${workflows}</tbody></table></div>${mismatch}</details>`;
        })
        .join('\n')
    : '<p class="muted">No private-repository usage this cycle.</p>';
  const publicRepos = /** @type {any[]} */ (usage.publicRepos);
  const publicHtml = publicRepos.length
    ? `<div class="table-scroll"><table><thead><tr><th>Repository</th><th>Runs</th><th>Raw</th><th>Billing</th></tr></thead><tbody>${publicRepos
        .map((repo) => `<tr><td>${esc(repo.repo)}</td><td>${fig(repo.runs, 'runs', '[run timing]')}</td><td>${fig(repo.raw, 'min', '[run timing]')}</td><td>${repo.largerRunner ? 'Larger runner used: billed' : 'Free'}</td></tr>`)
        .join('')}</tbody></table></div>`
    : '<p class="muted">No public-repository runs this cycle.</p>';
  return `<h3 class="sub">Private repositories <span class="muted">count against included minutes</span></h3>${privateHtml}
<h3 class="sub">Public repositories <span class="muted">free on standard GitHub-hosted runners</span></h3>${publicHtml}
<p class="muted">${esc(usage.idle.private)} private and ${esc(usage.idle.public)} public repositories had no runs this cycle.</p>`;
}

/**
 * @param {any} risk
 * @returns {string}
 */
function riskSection(risk) {
  const findings = /** @type {any[]} */ (risk.findings);
  const cards = findings
    .map((finding, index) => {
      const storage = finding.kind === 'storage';
      const unit = storage ? 'GB' : 'min';
      const perRunTag = finding.perRunSource === 'timing' ? '[run timing]' : '[estimate]';
      return `<article class="finding">
  <header><span class="rank">${index + 1}</span><h3>${esc(finding.title)}</h3></header>
  <dl class="metrics">
    <div><dt>${storage ? 'Per upload' : 'Per run'}</dt><dd>${finding.perRun === null ? '<span class="muted">n/a</span>' : fig(finding.perRun, unit, perRunTag)}</dd></div>
    <div><dt>${storage ? 'Held' : 'Per month'}</dt><dd>${fig(finding.monthly, unit, '[estimate]')}</dd></div>
    <div class="saving"><dt>Saving</dt><dd>~${fig(finding.saving, storage ? 'GB' : 'min/month', '[estimate]')}</dd></div>
  </dl>
  <p class="evidence">Evidence: <code>${esc(finding.evidence)}</code></p>
  <p class="change-label">Change</p>
  <pre><code>${esc(finding.change)}</code></pre>
  ${finding.savingNote ? `<p class="muted">${esc(finding.savingNote)}</p>` : ''}
</article>`;
    })
    .join('\n');
  const savedShare = risk.monthlyNow > 0 ? Math.min(100, (risk.combinedSaving / risk.monthlyNow) * 100) : 0;
  const combined =
    risk.combinedSaving > 0
      ? `<div class="combined">
  <p><strong>All minute changes together</strong> save ~${fig(risk.combinedSaving, 'min/month', '[estimate]')} of ${fig(risk.monthlyNow, 'min/month', '[estimate]')}. Savings overlap, so this is less than the sum of the cards.</p>
  <div class="combined-bar" role="img" aria-label="${esc(`${pct(savedShare)} of monthly minutes saved`)}"><span style="width:${savedShare.toFixed(2)}%"></span></div>
</div>`
      : '';
  const notes = [...risk.notes.map((/** @type {string} */ note) => `<li>${esc(note)}</li>`)].join('');
  const list = (/** @type {any[]} */ items) =>
    items
      .map((item) => `<li><strong>${esc(item.title)}</strong> <code>${esc(item.evidence)}</code><br>${esc(item.detail).replace(/\n/g, '<br>')}</li>`)
      .join('');
  return `<p class="lede">${fig(risk.runs, 'runs', '[run timing]')} and ${fig(risk.jobRuns, 'job runs', '[run timing]')} in the last ${esc(risk.windowDays)} days: ${fig(risk.rawTotal, 'min raw', '[run timing]')}, ${fig(risk.weightedTotal, 'min cost-weighted', '[estimate]')}.
History covers ${fig(risk.observedDays, 'days', '[run timing]', 1)}${risk.observedDays < risk.windowDays ? ` (repo created ${esc(risk.repoCreatedAt.slice(0, 10))})` : ''}; monthly figures scale that to 30 days.</p>
${risk.visibility === 'public' ? '<p class="callout">Public repository: standard runners are free here. Minutes show what the same workflows would cost in a private repository.</p>' : ''}
<p class="muted">Workflows reviewed: ${risk.workflows.map((/** @type {any} */ workflow) => `<code>${esc(workflow.path)}</code> @ ${esc(workflow.ref)}`).join(', ') || 'none found'}</p>
${notes ? `<ul class="notes">${notes}</ul>` : ''}
${findings.length ? `<div class="findings">${cards}</div>` : '<p>No findings with observed cost in the window.</p>'}
${combined}
${risk.latent.length ? `<h3 class="sub">Latent risks <span class="muted">no cost observed in the window yet</span></h3><ul class="plain">${list(risk.latent)}</ul>` : ''}
${risk.ok.length ? `<details><summary>Checked, no action needed (${esc(risk.ok.length)})</summary><ul class="plain">${list(risk.ok)}</ul></details>` : ''}`;
}

/**
 * @param {any} usage
 * @param {any | null} risk
 * @param {{ standalone: boolean, sources: { label: string, url: string }[], readDate: string }} options
 *   standalone: wrap in a full HTML document (local file); false: page content only (Artifact publish).
 * @returns {string}
 */
export function renderReport(usage, risk, options) {
  const worst = usage.limits.reduce(
    (/** @type {string} */ current, /** @type {any} */ limit) =>
      STATUS_RANK[/** @type {keyof typeof STATUS_RANK} */ (limit.status)] > STATUS_RANK[/** @type {keyof typeof STATUS_RANK} */ (current)] ? limit.status : current,
    'fine',
  );
  const cycle = usage.cycle;
  const cycleEndInclusive = new Date(Date.parse(cycle.end) - 1).toISOString().slice(0, 10);
  const elapsedShare = Math.min(100, (cycle.daysElapsed / cycle.daysInCycle) * 100);
  const weighted = usage.limits.find((/** @type {any} */ limit) => limit.key === 'minutes-weighted');
  const thresholds = usage.thresholds;
  const title = `${usage.account} Actions Usage`;

  const head = `<title>${esc(title)}</title>
<meta name="claude:viewport-fit-sides" content="cover">
<style>
/* Layout: one reading column; summary band (status + limit meters) first, then charts, breakdown, risk review. */
:root {
  --bg: #f6f6f3; --surface: #fcfcfb; --ink: #121211; --ink-2: #52514e; --muted: #807e78;
  --grid: #e3e2db; --rule: #c3c2b7; --accent: #1c5cab; --code-bg: #efeee9;
  --series-1: #2a78d6; --series-2: #eb6834; --series-3: #1baf7a;
  --good: #0a7d0a; --warn: #9a6400; --crit: #c02f2f; --warn-fill: #fab219; --good-fill: #0ca30c; --crit-fill: #d03b3b;
  --ring: rgba(18,18,17,0.10);
  --font-ui: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  --font-mono: ui-monospace, "SF Mono", Menlo, Consolas, monospace;
}
@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) {
  --bg: #0f0f0e; --surface: #1a1a19; --ink: #f0efec; --ink-2: #c3c2b7; --muted: #94928b;
  --grid: #2c2c2a; --rule: #45453f; --accent: #86b6ef; --code-bg: #242422;
  --series-1: #3987e5; --series-2: #d95926; --series-3: #199e70;
  --good: #3cc23c; --warn: #f2b33d; --crit: #ef7373; --ring: rgba(255,255,255,0.10); color-scheme: dark;
} }
:root[data-theme="dark"] {
  --bg: #0f0f0e; --surface: #1a1a19; --ink: #f0efec; --ink-2: #c3c2b7; --muted: #94928b;
  --grid: #2c2c2a; --rule: #45453f; --accent: #86b6ef; --code-bg: #242422;
  --series-1: #3987e5; --series-2: #d95926; --series-3: #199e70;
  --good: #3cc23c; --warn: #f2b33d; --crit: #ef7373; --ring: rgba(255,255,255,0.10); color-scheme: dark;
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--ink); font: 15px/1.5 var(--font-ui); }
.page { max-width: 1080px; margin: 0 auto; padding-block: 28px 56px;
  padding-left: max(16px, env(safe-area-inset-left, 0px)); padding-right: max(16px, env(safe-area-inset-right, 0px));
  display: grid; grid-template-columns: minmax(0, 1fr); gap: 36px; }
.masthead, .page > section { min-width: 0; }
h1, h2, h3 { text-wrap: balance; margin: 0; }
h1 { font-size: clamp(1.6rem, 3.2vw, 2.2rem); font-weight: 700; letter-spacing: -0.02em; line-height: 1.15; }
h2 { font-size: 1.15rem; font-weight: 650; letter-spacing: -0.01em; }
h3 { font-size: 0.98rem; font-weight: 600; }
h3.sub { margin-top: 8px; display: flex; flex-wrap: wrap; gap: 4px 10px; align-items: baseline; }
p { margin: 0; }
a { color: var(--accent); }
a:focus-visible, summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 3px; }
code, pre { font-family: var(--font-mono); font-size: 0.84em; }
code { background: var(--code-bg); padding: 1px 5px; border-radius: 4px; overflow-wrap: anywhere; }
pre { background: var(--code-bg); padding: 12px 14px; border-radius: 6px; overflow-x: auto; margin: 0; line-height: 1.45; }
pre code { background: none; padding: 0; overflow-wrap: normal; }
.muted { color: var(--muted); font-weight: 400; }
.eyebrow { font-size: 0.75rem; letter-spacing: 0.08em; text-transform: uppercase; color: var(--ink-2); font-weight: 600; }
.fig { white-space: nowrap; font-variant-numeric: tabular-nums; }
.src { font-size: 0.72em; color: var(--muted); font-weight: 500; letter-spacing: 0.01em; white-space: nowrap; }
section { display: grid; gap: 16px; min-width: 0; }
section > header { display: grid; gap: 4px; border-top: 1px solid var(--rule); padding-top: 14px; }

.masthead { display: grid; gap: 14px; }
.masthead-row { display: flex; flex-wrap: wrap; gap: 12px 20px; align-items: center; justify-content: space-between; }
.cycle { display: grid; gap: 6px; max-width: 560px; }
.cycle-bar { height: 6px; background: var(--grid); border-radius: 3px; overflow: hidden; }
.cycle-bar span { display: block; height: 100%; background: var(--ink-2); border-radius: 3px; }
.cycle-meta { display: flex; justify-content: space-between; gap: 12px; font-size: 0.85rem; color: var(--ink-2); }
.verdict { font-size: 1.05rem; color: var(--ink-2); max-width: 70ch; }
.verdict strong { color: var(--ink); }
.notice { border-left: 3px solid var(--warn-fill); background: var(--surface); padding: 10px 14px; border-radius: 0 6px 6px 0; font-family: var(--font-mono); font-size: 0.82rem; white-space: pre-wrap; }

.pill { display: inline-flex; align-items: center; gap: 6px; padding: 3px 10px 3px 8px; border-radius: 999px; font-size: 0.8rem; font-weight: 650; border: 1px solid currentColor; white-space: nowrap; }
.pill svg { width: 14px; height: 14px; }
.pill-fine { color: var(--good); } .pill-watch { color: var(--warn); } .pill-risk { color: var(--crit); }
.masthead .pill { font-size: 0.95rem; padding: 5px 14px 5px 11px; }
.masthead .pill svg { width: 16px; height: 16px; }

.limits { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 14px; }
@media (max-width: 960px) { .limits { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 520px) { .limits { grid-template-columns: minmax(0, 1fr); } }
.limit { background: var(--surface); border: 1px solid var(--ring); border-radius: 8px; padding: 14px 16px 12px; display: grid; gap: 6px; align-content: start; min-width: 0; }
.limit header { display: flex; justify-content: space-between; gap: 8px; align-items: center; }
.limit h3 { font-size: 0.86rem; color: var(--ink-2); font-weight: 600; }
.limit-used { font-size: 1.65rem; font-weight: 700; letter-spacing: -0.02em; line-height: 1.2; }
.limit-used .src { font-size: 0.42em; vertical-align: middle; }
.limit-of, .limit-proj { font-size: 0.82rem; color: var(--ink-2); }
.meter { position: relative; height: 12px; background: var(--grid); border-radius: 6px; margin-top: 8px; }
.meter-fill { position: absolute; inset: 0 auto 0 0; border-radius: 6px; min-width: 3px; }
.meter-fine { background: var(--good-fill); } .meter-watch { background: var(--warn-fill); } .meter-risk { background: var(--crit-fill); }
.tick { position: absolute; top: -3px; bottom: -3px; width: 2px; margin-left: -1px; background: var(--ink-2); opacity: 0.55; }
.tick-limit { background: var(--ink); opacity: 0.9; }
.proj { position: absolute; top: -5px; width: 10px; height: 10px; margin-left: -5px; transform: rotate(45deg); background: var(--surface); border: 2px solid var(--ink); }
.proj-key { display: inline-block; width: 8px; height: 8px; transform: rotate(45deg); border: 2px solid var(--ink); margin-right: 7px; vertical-align: 1px; }
.meter-scale { position: relative; height: 14px; font-size: 0.68rem; color: var(--muted); font-variant-numeric: tabular-nums; }
.meter-scale span { position: absolute; transform: translateX(-50%); }
.meter-scale span:first-child { transform: none; left: 0; }
.meter-scale .end { right: 0; transform: none; }

.chart-wrap { margin: 0; display: grid; gap: 10px; min-width: 0; }
.chart-scroll { position: relative; overflow-x: auto; background: var(--surface); border: 1px solid var(--ring); border-radius: 8px; padding: 8px; }
.chart { display: block; width: 100%; min-width: 520px; height: auto; }
.chart text { font-family: var(--font-ui); }
.grid { stroke: var(--grid); stroke-width: 1; }
.baseline { stroke: var(--rule); stroke-width: 1; }
.axis-label { fill: var(--muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.included { stroke: var(--ink-2); stroke-width: 1.5; }
.included-label { fill: var(--ink-2); font-size: 12px; font-weight: 600; }
.today { stroke: var(--rule); stroke-width: 1; }
.area { fill: var(--series-1); opacity: 0.12; }
.line { fill: none; stroke: var(--series-1); stroke-width: 2.25; stroke-linejoin: round; }
.projection { fill: none; stroke: var(--series-1); stroke-width: 2; stroke-dasharray: 6 5; opacity: 0.85; }
.endpoint { fill: var(--series-1); stroke: var(--surface); stroke-width: 2; }
.proj-end { fill: var(--surface); stroke: var(--series-1); stroke-width: 2; }
.value-label { fill: var(--ink); font-size: 12px; font-weight: 600; }
.crosshair { stroke: var(--ink-2); stroke-width: 1; }
.tooltip { position: absolute; top: 10px; pointer-events: none; background: var(--ink); color: var(--bg); padding: 7px 10px; border-radius: 6px; font-size: 0.78rem; line-height: 1.4; white-space: nowrap; font-variant-numeric: tabular-nums; }
figcaption { font-size: 0.82rem; color: var(--ink-2); max-width: 80ch; }

.stacks { display: grid; gap: 14px; background: var(--surface); border: 1px solid var(--ring); border-radius: 8px; padding: 14px 16px; }
.stack-row { display: grid; gap: 6px; }
.stack-head { display: flex; justify-content: space-between; gap: 10px; font-size: 0.85rem; color: var(--ink-2); }
.stack { display: flex; gap: 2px; height: 26px; }
.seg { display: flex; align-items: center; min-width: 4px; border-radius: 3px; overflow: hidden; }
.seg-label { color: #fff; font-size: 0.75rem; font-weight: 650; padding: 0 8px; white-space: nowrap; text-shadow: 0 0 2px rgba(0,0,0,0.35); }
.legend { list-style: none; display: flex; flex-wrap: wrap; gap: 6px 18px; padding: 0; margin: 0; font-size: 0.85rem; color: var(--ink-2); }
.swatch { display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 7px; vertical-align: -1px; }

.table-scroll { overflow-x: auto; min-width: 0; }
table { border-collapse: collapse; width: 100%; font-size: 0.86rem; }
th { text-align: left; font-weight: 600; color: var(--ink-2); font-size: 0.76rem; letter-spacing: 0.04em; text-transform: uppercase; border-bottom: 1px solid var(--rule); padding: 8px 10px 6px; white-space: nowrap; }
td { border-bottom: 1px solid var(--grid); padding: 7px 10px; vertical-align: top; }
tr.wf td { font-weight: 600; background: var(--surface); }
tr.job td:first-child { padding-left: 26px; color: var(--ink-2); }
details > summary { cursor: pointer; color: var(--ink-2); font-size: 0.88rem; padding: 4px 0; }
details.repo { background: var(--surface); border: 1px solid var(--ring); border-radius: 8px; padding: 10px 14px; }
details.repo > summary { display: flex; flex-wrap: wrap; gap: 4px 14px; align-items: baseline; color: var(--ink); }
.repo-name { font-weight: 650; }
.repo-figs { font-size: 0.85rem; color: var(--ink-2); }

.lede { max-width: 80ch; color: var(--ink-2); }
.callout { border-left: 3px solid var(--series-1); padding: 6px 12px; background: var(--surface); }
.notes, .plain { margin: 0; padding-left: 18px; display: grid; gap: 8px; font-size: 0.88rem; color: var(--ink-2); }
.plain code { font-size: 0.78em; }
.findings { display: grid; gap: 14px; }
.finding { background: var(--surface); border: 1px solid var(--ring); border-radius: 8px; padding: 16px 18px; display: grid; gap: 10px; min-width: 0; }
.finding header { display: flex; gap: 12px; align-items: baseline; }
.rank { flex: none; font-variant-numeric: tabular-nums; font-weight: 700; color: var(--muted); font-size: 0.9rem; min-width: 1.4em; }
.finding h3 { font-size: 1rem; }
.metrics { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; margin: 0; }
@media (max-width: 520px) { .metrics { grid-template-columns: minmax(0, 1fr); } }
.metrics div { display: grid; gap: 2px; }
.metrics dt { font-size: 0.72rem; letter-spacing: 0.06em; text-transform: uppercase; color: var(--muted); font-weight: 600; }
.metrics dd { margin: 0; font-weight: 650; font-size: 1.02rem; }
.metrics .saving dd { color: var(--good); }
.evidence { font-size: 0.82rem; color: var(--ink-2); }
.change-label { font-size: 0.72rem; letter-spacing: 0.06em; text-transform: uppercase; color: var(--muted); font-weight: 600; margin-bottom: -4px; }
.combined { display: grid; gap: 8px; background: var(--surface); border: 1px solid var(--ring); border-radius: 8px; padding: 14px 16px; }
.combined-bar { height: 10px; background: var(--grid); border-radius: 5px; overflow: hidden; }
.combined-bar span { display: block; height: 100%; background: var(--good-fill); border-radius: 5px; }
.sources { margin: 0; padding-left: 18px; display: grid; gap: 4px; font-size: 0.86rem; }
footer.colophon { font-size: 0.8rem; color: var(--muted); border-top: 1px solid var(--rule); padding-top: 12px; }
@media (prefers-reduced-motion: reduce) { * { transition: none !important; animation: none !important; } }
</style>`;

  const verdict = `<p class="verdict"><strong>${esc(pct(weighted.usedPct))}</strong> of the ${esc(num(weighted.included))} included minutes used (cost-weighted), with ${esc(num(cycle.daysLeft, 1))} days left. At the current rate the cycle ends at <strong>${esc(pct(weighted.projectedPct))}</strong>.</p>`;
  const notices = usage.notices.length ? usage.notices.map((/** @type {string} */ notice) => `<p class="notice">${esc(notice)}</p>`).join('') : '';

  const body = `<main class="page">
<header class="masthead">
  <p class="eyebrow">GitHub Actions &middot; ${esc(usage.plan)} plan &middot; ${esc(usage.account)}</p>
  <div class="masthead-row"><h1>${esc(title)}</h1>${statusPill(worst)}</div>
  ${verdict}
  <div class="cycle" aria-label="billing cycle progress">
    <div class="cycle-bar"><span style="width:${elapsedShare.toFixed(2)}%"></span></div>
    <div class="cycle-meta"><span>Cycle ${esc(cycle.start.slice(0, 10))} to ${esc(cycleEndInclusive)} (UTC calendar month)</span><span>${fig(cycle.daysLeft, 'days left', '[docs]', 1)}</span></div>
  </div>
  ${notices}
</header>

<section aria-labelledby="limits-h">
  <header><h2 id="limits-h">Limits this cycle</h2><p class="muted">Bar: used. Diamond: projected at cycle end. Ticks: watch at ${esc(thresholds.watchUsedPct)}%, at risk at ${esc(thresholds.atRiskUsedPct)}%.</p></header>
  <div class="limits">${usage.limits.map((/** @type {any} */ limit) => limitCard(limit, thresholds)).join('\n')}</div>
</section>

<section aria-labelledby="trend-h">
  <header><h2 id="trend-h">Minutes over the cycle</h2></header>
  ${cumulativeChart(usage)}
</section>

<section aria-labelledby="os-h">
  <header><h2 id="os-h">Minutes by runner OS</h2><p class="muted">Cost-weighted = raw minutes x (runner rate / Linux 2-core rate).</p></header>
  ${osChart(usage)}
</section>

<section aria-labelledby="repos-h">
  <header><h2 id="repos-h">Where the minutes went</h2></header>
  ${breakdown(usage)}
</section>

${risk ? `<section aria-labelledby="risk-h">
  <header><p class="eyebrow">Risk review</p><h2 id="risk-h">${esc(risk.repo)}</h2></header>
  ${riskSection(risk)}
</section>` : ''}

<section aria-labelledby="notes-h">
  <header><h2 id="notes-h">How to read this</h2></header>
  <ul class="notes">
    <li>Source tags: <span class="src">[billing API]</span> GitHub's billing usage report; <span class="src">[run timing]</span> job start and end times, rounded up per job; <span class="src">[estimate]</span> derived here; <span class="src">[docs]</span> GitHub's published allowances and cycle rules.</li>
    <li>GitHub states included usage in minutes but no longer documents how a macOS or Windows minute counts against it. Status uses the cost-weighted figure, which is the conservative one.</li>
    <li>Projection: used / days elapsed (at least ${esc(usage.minElapsedDays)} day) x ${esc(cycle.daysInCycle)} days in the cycle.</li>
    <li>Status: fine under ${esc(thresholds.watchUsedPct)}% used and projected under ${esc(thresholds.watchProjectedPct)}%; watch from ${esc(thresholds.watchUsedPct)}% used or projected ${esc(thresholds.watchProjectedPct)}-${esc(thresholds.atRiskProjectedPct)}%; at risk from ${esc(thresholds.atRiskUsedPct)}% used or projected over ${esc(thresholds.atRiskProjectedPct)}%.</li>
    <li>${usage.storageFromBilling ? 'Artifact storage: GB-months accrued so far from billing GB-hours.' : 'Artifact storage: snapshot of unexpired artifacts in private repositories (no storage rows in the billing API this cycle). GitHub Packages shares this allowance and is not counted.'}</li>
  </ul>
  <p class="muted">Quotas and rules read from GitHub's docs on ${esc(options.readDate)}:</p>
  <ul class="sources">${options.sources.map((source) => `<li><a href="${esc(source.url)}">${esc(source.label)}</a></li>`).join('')}</ul>
</section>

<footer class="colophon">Generated ${esc(usage.generatedAt.replace('T', ' ').slice(0, 16))} UTC by the github-actions-usage skill. Read-only: nothing on GitHub was changed.</footer>
</main>
<script>
(function () {
  var wrap = document.querySelector('[data-chart]');
  if (!wrap) return;
  var data = JSON.parse(wrap.getAttribute('data-chart'));
  var svg = wrap.querySelector('svg');
  var hit = svg.querySelector('.hit');
  var line = svg.querySelector('.crosshair');
  var tip = wrap.querySelector('.tooltip');
  function fmt(n) { return Math.round(n).toLocaleString('en-US'); }
  function show(event) {
    var box = svg.getBoundingClientRect();
    var scale = data.width / box.width;
    var vx = (event.clientX - box.left) * scale;
    var day = Math.floor(((vx - data.left) / data.plotWidth) * data.daysInCycle);
    var row = data.rows[Math.max(0, Math.min(data.rows.length - 1, day))];
    if (!row || day >= data.rows.length + 1) { hide(); return; }
    var x = data.left + ((row.day + 0.5) / data.daysInCycle) * data.plotWidth;
    line.setAttribute('x1', x); line.setAttribute('x2', x); line.setAttribute('visibility', 'visible');
    tip.textContent = '';
    var lines = [row.date + ' (UTC)', '+' + fmt(row.weighted) + ' min cost-weighted [estimate]', '+' + fmt(row.raw) + ' min raw ' + data.source, 'Cumulative ' + fmt(row.cumulative) + ' min [estimate]'];
    lines.forEach(function (text, index) { var div = document.createElement('div'); div.textContent = text; if (index === 0) div.style.fontWeight = '650'; tip.appendChild(div); });
    tip.hidden = false;
    var px = (x / scale) + svg.getBoundingClientRect().left - wrap.getBoundingClientRect().left + wrap.scrollLeft;
    var left = px + 12;
    if (left + tip.offsetWidth > wrap.scrollLeft + wrap.clientWidth - 8) left = px - tip.offsetWidth - 12;
    tip.style.left = left + 'px';
  }
  function hide() { line.setAttribute('visibility', 'hidden'); tip.hidden = true; }
  hit.addEventListener('pointermove', show);
  hit.addEventListener('pointerdown', show);
  hit.addEventListener('pointerleave', hide);
})();
</script>`;

  if (!options.standalone) return `${head}\n${body}\n`;
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
${head}
</head>
<body>
${body}
</body>
</html>
`;
}
