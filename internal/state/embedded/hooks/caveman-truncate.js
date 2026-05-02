#!/usr/bin/env node
// caveman PreToolUse — truncate large tool outputs before they're forwarded
// back to Claude. Saves tokens; preserves the full result on disk.
//
// Claude Code calls this hook after a tool runs. It receives a JSON object on
// stdin shaped roughly like:
//   { "tool_name": "Bash", "tool_input": {...}, "tool_response": { "stdout": "...", "stderr": "..." } }
//
// We rewrite tool_response.<text fields> to caveman-truncated copies if they
// exceed the per-tool cap configured in ~/.yashigatakae/caveman.json. Untouched
// objects (e.g. exit codes, file paths) pass through.
//
// Truncation is delegated to the yashigatakae binary so a single config drives
// both the CLI dry-run and the live hook.

const { execSync, spawnSync } = require('child_process');
const fs = require('fs');

let payload;
try {
  const raw = fs.readFileSync(0, 'utf8');
  payload = JSON.parse(raw || '{}');
} catch (e) {
  process.exit(0); // bad input — never block tool flow
}

const tool = payload.tool_name || '';
if (!tool) {
  process.stdout.write(JSON.stringify(payload));
  process.exit(0);
}

function truncate(text) {
  try {
    const r = spawnSync('yashigatakae', ['caveman', 'truncate', '--tool', tool, '--json'], {
      input: text,
      encoding: 'utf8',
      timeout: 5000,
    });
    if (r.status !== 0 || !r.stdout) return text;
    const res = JSON.parse(r.stdout);
    return res.output || text;
  } catch (e) {
    return text; // never break the tool because caveman missed
  }
}

function walk(obj) {
  if (obj == null) return obj;
  if (typeof obj === 'string') {
    if (obj.length < 1024) return obj; // tiny strings: skip the round trip
    return truncate(obj);
  }
  if (Array.isArray(obj)) return obj.map(walk);
  if (typeof obj === 'object') {
    const out = {};
    for (const k of Object.keys(obj)) out[k] = walk(obj[k]);
    return out;
  }
  return obj;
}

if (payload.tool_response) {
  payload.tool_response = walk(payload.tool_response);
}

process.stdout.write(JSON.stringify(payload));
