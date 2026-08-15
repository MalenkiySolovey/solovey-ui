#!/usr/bin/env bash
set -u

ROOT_DIR="${1:-.}"
OUT_DIR="${AUDIT_OUT_DIR:-tests/baseline/current}"

mkdir -p "$ROOT_DIR/$OUT_DIR"

node - "$ROOT_DIR" "$OUT_DIR" <<'NODE'
const fs = require('fs');
const path = require('path');

const root = path.resolve(process.argv[2] || '.');
const outDir = path.resolve(root, process.argv[3] || 'tests/baseline/current');
const baselineDir = path.join(root, 'tests', 'baseline');
const summaryPath = path.join(baselineDir, 'SUMMARY.md');

function walk(dir) {
  if (!fs.existsSync(dir)) return [];
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) files.push(...walk(full));
    if (entry.isFile()) files.push(full);
  }
  return files;
}

function read(file) {
  try {
    return fs.readFileSync(file, 'utf8');
  } catch {
    return '';
  }
}

function attrs(input) {
  const out = {};
  input.replace(/([A-Za-z_:][-A-Za-z0-9_:.]*)="([^"]*)"/g, (_, key, value) => {
    out[key] = value;
    return '';
  });
  return out;
}

function num(value) {
  const n = Number(value || 0);
  return Number.isFinite(n) ? n : 0;
}

function groupOf(file) {
  const rel = path.relative(baselineDir, file).split(path.sep);
  return rel[0] || 'unknown';
}

function countMarkers(text, pattern) {
  const matches = text.match(pattern);
  return matches ? matches.length : 0;
}

function escapeHtml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

fs.mkdirSync(outDir, { recursive: true });

const junitFiles = walk(baselineDir)
  .filter((file) => file.endsWith('.junit.xml') && !file.startsWith(`${outDir}${path.sep}`))
  .sort();

const byGroup = new Map();
const files = [];
let totals = { tests: 0, green: 0, red: 0, skipped: 0, xfail: 0 };

for (const file of junitFiles) {
  const xml = read(file);
  const suites = [...xml.matchAll(/<testsuite\b([^>]*)>/g)];
  let tests = 0;
  let failures = 0;
  let errors = 0;
  let skipped = 0;

  if (suites.length > 0) {
    for (const suite of suites) {
      const a = attrs(suite[1]);
      tests += num(a.tests);
      failures += num(a.failures);
      errors += num(a.errors);
      skipped += num(a.skipped);
    }
  } else {
    tests = countMarkers(xml, /<testcase\b/g);
    failures = countMarkers(xml, /<failure\b/g);
    errors = countMarkers(xml, /<error\b/g);
    skipped = countMarkers(xml, /<skipped\b/g);
  }

  const txtFile = file.replace(/\.junit\.xml$/, '.txt');
  const txt = read(txtFile);
  const xfail = Math.max(
    countMarkers(xml, /<skipped\b[^>]*message="expected failure"/gi),
    countMarkers(txt, /^# Status: xfail$/gim),
  );
  const red = failures + errors;
  const green = Math.max(0, tests - red - skipped);
  const group = groupOf(file);

  if (!byGroup.has(group)) {
    byGroup.set(group, { group, tests: 0, green: 0, red: 0, skipped: 0, xfail: 0, files: 0 });
  }
  const bucket = byGroup.get(group);
  bucket.tests += tests;
  bucket.green += green;
  bucket.red += red;
  bucket.skipped += skipped;
  bucket.xfail += xfail;
  bucket.files += 1;

  totals.tests += tests;
  totals.green += green;
  totals.red += red;
  totals.skipped += skipped;
  totals.xfail += xfail;

  files.push({
    group,
    file: path.relative(root, file).replace(/\\/g, '/'),
    tests,
    green,
    red,
    skipped,
    xfail,
  });
}

const summaryMd = read(summaryPath);
const baselineMarkers = {
  green: countMarkers(summaryMd, /\bgreen\b/gi),
  red: countMarkers(summaryMd, /\bred\b/gi),
  skipped: countMarkers(summaryMd, /\bskip(?:ped)?\b/gi),
  xfail: countMarkers(summaryMd, /\bXFAIL\b/gi),
};

const groups = [...byGroup.values()].sort((a, b) => a.group.localeCompare(b.group));
const result = {
  generatedAt: new Date().toISOString(),
  root,
  summaryPath: path.relative(root, summaryPath).replace(/\\/g, '/'),
  totals,
  baselineMarkers,
  deltaVsBaselineMarkers: {
    green: totals.green - baselineMarkers.green,
    red: totals.red - baselineMarkers.red,
    skipped: totals.skipped - baselineMarkers.skipped,
    xfail: totals.xfail - baselineMarkers.xfail,
  },
  groups,
  files,
};

fs.writeFileSync(path.join(outDir, 'summary.json'), `${JSON.stringify(result, null, 2)}\n`);

const rows = groups.map((group) => `
      <tr>
        <td>${escapeHtml(group.group)}</td>
        <td>${group.files}</td>
        <td>${group.tests}</td>
        <td class="green">${group.green}</td>
        <td class="red">${group.red}</td>
        <td>${group.skipped}</td>
        <td>${group.xfail}</td>
      </tr>`).join('');

const html = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <title>Solovey UI audit dashboard</title>
  <style>
    body { font-family: system-ui, -apple-system, Segoe UI, sans-serif; margin: 32px; color: #17202a; }
    table { border-collapse: collapse; width: 100%; margin-top: 16px; }
    th, td { border: 1px solid #d8dee4; padding: 8px 10px; text-align: left; }
    th { background: #f6f8fa; }
    .green { color: #1a7f37; font-weight: 600; }
    .red { color: #cf222e; font-weight: 600; }
    .meta { color: #57606a; }
    code { background: #f6f8fa; padding: 2px 4px; border-radius: 4px; }
  </style>
</head>
<body>
  <h1>Solovey UI audit dashboard</h1>
  <p class="meta">Generated at ${escapeHtml(result.generatedAt)}</p>
  <p>Totals: <span class="green">${totals.green} green</span>, <span class="red">${totals.red} red</span>, ${totals.skipped} skipped, ${totals.xfail} XFAIL markers.</p>
  <p class="meta">Baseline marker delta: green ${result.deltaVsBaselineMarkers.green}, red ${result.deltaVsBaselineMarkers.red}, skipped ${result.deltaVsBaselineMarkers.skipped}, XFAIL ${result.deltaVsBaselineMarkers.xfail}.</p>
  <table>
    <thead>
      <tr>
        <th>Group</th>
        <th>JUnit files</th>
        <th>Tests</th>
        <th>Green</th>
        <th>Red</th>
        <th>Skipped</th>
        <th>XFAIL markers</th>
      </tr>
    </thead>
    <tbody>${rows}
    </tbody>
  </table>
  <p class="meta">Raw JSON: <code>summary.json</code></p>
</body>
</html>
`;

fs.writeFileSync(path.join(outDir, 'summary.html'), html);

const aggregateFailed = totals.red > 0;
const junit = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="audit-aggregate" tests="1" failures="${aggregateFailed ? 1 : 0}" errors="0" skipped="0">
  <testcase classname="audit.aggregate" name="aggregate">${aggregateFailed ? `
    <failure message="unexpected audit failures">${totals.red} unexpected audit result(s)</failure>` : ''}
  </testcase>
</testsuite>
`;
fs.writeFileSync(path.join(outDir, 'aggregate.junit.xml'), junit);

console.log(`Audit dashboard written to ${path.relative(root, path.join(outDir, 'summary.html')).replace(/\\/g, '/')}`);
console.log(`Totals: green=${totals.green} red=${totals.red} skipped=${totals.skipped} xfail=${totals.xfail}`);
if (aggregateFailed) {
  console.error(`Unexpected audit failures: ${totals.red}`);
  process.exitCode = 1;
}
NODE
