import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// Pure DOM/selector boundary, without a browser, server or business fixture.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const require = createRequire(import.meta.url);
const { JSDOM, VirtualConsole } = require(resolve(root, 'web/node_modules/jsdom'));
const { source } = require(resolve(root, 'tests/account-captcha-web/node_modules/playwright-core/lib/generated/injectedScriptSource.js'));
const template = readFileSync(resolve(root, 'web/src/components/ui/UiField.vue'), 'utf8');
if (!template.includes('<span v-if="required" aria-hidden="true"> *</span>')) throw new Error('DELIVERED_UI_FIELD_SHAPE_CHANGED');
const dialogTemplate = readFileSync(resolve(root, 'web/src/components/ui/UiDialog.vue'), 'utf8');
const credentialTemplate = readFileSync(resolve(root, 'web/src/views/projects/ProjectModelCredentialEditor.vue'), 'utf8');
if (!dialogTemplate.includes('<UiButton icon variant="ghost" aria-label="关闭"') || !credentialTemplate.includes('<UiButton :disabled="blocked" @click="page.closeCredential()">关闭</UiButton')) throw new Error('DELIVERED_CREDENTIAL_CLOSE_SHAPE_CHANGED');
const unsupported = [];
const console = new VirtualConsole();
console.on('jsdomError', () => unsupported.push('jsdom-pseudo-style-not-implemented'));
const dom = new JSDOM('<label for="provider">Provider 名称<span aria-hidden="true"> *</span></label><input id="provider"><label for="credential">新凭据材料<span aria-hidden="true"> *</span></label><input id="credential" type="password"><section role="dialog"><header><button aria-label="关闭"></button></header><form id="project-credential-form"></form><footer><button>关闭</button><button>轮换凭据</button></footer></section>', { runScripts: 'outside-only', virtualConsole: console });
try {
  dom.window.eval('var module={exports:{}};' + source + ';window.__labelProbe={getElementLabels,getElementAccessibleName};');
  const input = dom.window.document.getElementById('provider');
  const label = dom.window.__labelProbe.getElementLabels(new Map(), input)[0].normalized;
  const name = dom.window.__labelProbe.getElementAccessibleName(input);
  const credential = dom.window.__labelProbe.getElementLabels(new Map(), dom.window.document.getElementById('credential'))[0].normalized;
  // This locked-template projection explains the selector ambiguity; it does
  // not reconstruct the DOM from a previous real browser failure.
  const closeMatches = (selector) => [...dom.window.document.querySelectorAll(selector)].filter((node) => dom.window.__labelProbe.getElementAccessibleName(node) === '关闭').length;
  const facts = { exact_label_matches: label === 'Provider 名称', exact_role_name_matches: name === 'Provider 名称', protected_label_matches: /^新凭据材料(?:\s*\*)?$/.test(credential), required_label_includes_asterisk: label.endsWith(' *'), original_dialog_close_matches: closeMatches('[role="dialog"] button'), footer_close_matches: closeMatches('[role="dialog"] footer button'), unsupported: [...new Set(unsupported)], pure_dom_only: true };
  if (facts.exact_label_matches || !facts.exact_role_name_matches || !facts.protected_label_matches || !facts.required_label_includes_asterisk) throw new Error('LOCKED_SELECTOR_PROBE_FAILED');
  if (facts.original_dialog_close_matches !== 2 || facts.footer_close_matches !== 1) throw new Error('LOCKED_CLOSE_SELECTOR_PROBE_FAILED');
  process.stdout.write(JSON.stringify(facts) + '\n');
} finally { dom.window.close(); }
