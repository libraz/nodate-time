// Prints the nginx map that lists the script sources the SPA's CSP allows: its
// own origin plus the hash of each inline script in the built index.html.
// Usage: bun csp-script-hash.mjs <path to index.html>
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

const html = readFileSync(process.argv[2], 'utf8');
const sources = ["'self'"];
for (const [, attributes, body] of html.matchAll(/<script([^>]*)>([\s\S]*?)<\/script>/g)) {
  if (/\ssrc\s*=/.test(attributes)) continue;
  sources.push(`'sha256-${createHash('sha256').update(body).digest('base64')}'`);
}
process.stdout.write(`map $host $spa_script_src {\n    default "${sources.join(' ')}";\n}\n`);
