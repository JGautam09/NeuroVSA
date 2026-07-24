// Dependency-free security invariants for the RuleGarden web UI, run by CI (`node
// security.test.mjs`, no browser, no npm install). These are the P0-1 (stored-XSS) guards:
// the structural rule that untrusted pack content can never reach innerHTML, plus a strict
// CSP as defense in depth. The RUNTIME payload test (a malicious label renders as inert
// text) is driven in a real browser during verification; this file keeps the structural
// guarantees from regressing.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const read = (f) => readFileSync(join(here, f), 'utf8');
let failures = 0;
const fail = (m) => { console.error('  ✗ ' + m); failures++; };
const ok = (m) => console.log('  ✓ ' + m);

// stripComments removes // line and /* */ block comments so we only inspect real code.
function stripComments(src) {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/.*$/gm, '$1');
}

// 1. No `.innerHTML =` assignment anywhere in the RuleGarden JS. All rendering goes through
//    DOM createElement + textContent (the el() helper). This is the invariant that makes an
//    HTML/SVG/event-handler payload in a pack label inert.
for (const f of ['app.js', 'registry.js', 'sync.js', 'keys.js']) {
  const code = stripComments(read(f));
  if (/\.innerHTML\s*=/.test(code)) {
    fail(`${f}: found an .innerHTML assignment — untrusted content must render via DOM/textContent`);
  } else {
    ok(`${f}: no innerHTML assignment`);
  }
  // outerHTML / insertAdjacentHTML / document.write are the same hazard.
  for (const sink of ['.outerHTML', 'insertAdjacentHTML', 'document.write']) {
    if (code.includes(sink)) fail(`${f}: uses ${sink} (an HTML-injection sink)`);
  }
}

// 2. The el() helper must set text via textContent, never innerHTML.
const app = read('app.js');
if (/function el\(/.test(app) && /n\.textContent\s*=/.test(app)) {
  ok('app.js: el() builds nodes with textContent');
} else {
  fail('app.js: el() helper missing or not using textContent');
}

// 3. A strict CSP is present with script-src 'self' (blocks injected inline script) and no
//    'unsafe-inline'/'unsafe-eval' in script-src.
const html = read('index.html');
const csp = html.match(/Content-Security-Policy"\s+content="([^"]+)"/);
if (!csp) {
  fail('index.html: no Content-Security-Policy meta tag');
} else {
  const policy = csp[1];
  const scriptSrc = (policy.match(/script-src([^;]*)/) || [, ''])[1];
  if (!/'self'/.test(scriptSrc)) fail("CSP script-src must include 'self'");
  else if (/'unsafe-inline'|'unsafe-eval'\b/.test(scriptSrc)) fail('CSP script-src must not allow unsafe-inline/unsafe-eval');
  else ok("index.html: CSP script-src is 'self' (+ wasm-unsafe-eval)");
}

// 4. No inline event-handler attributes (onclick=, onerror=, ...) in the HTML — they would
//    require 'unsafe-inline' and are a classic injection vector.
if (/\son[a-z]+\s*=\s*["']/i.test(html.replace(/<!--[\s\S]*?-->/g, ''))) {
  fail('index.html: inline event-handler attribute found');
} else {
  ok('index.html: no inline event handlers');
}

if (failures) {
  console.error(`\nSECURITY CHECKS FAILED (${failures}).`);
  process.exit(1);
}
console.log('\nAll RuleGarden web security invariants hold.');
