import { createRequire } from 'node:module';
import { basename, join } from 'node:path';
import { readFile, readdir } from 'node:fs/promises';
const need = (value, code) => { if (!value) throw new Error(code); };

// Resolve the actual already-loaded production singleton, never a separately
// bundled controller. Ambiguous/missing exports fail before a browser starts.
export async function loginOwnerModule(root) {
  const ts = createRequire(join(root, 'web/package.json'))('typescript');
  const dist = join(root, 'output/ai/model-ui-recovery/dist'), matches = [];
  const parse = (text) => ts.createSourceFile('asset.js', text, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS);
  for (const file of (await readdir(join(dist, 'assets'))).filter(name => name.endsWith('.js')).sort()) {
    const code = await readFile(join(dist, 'assets', file), 'utf8');
    if (!code.includes('getSession') || !code.includes('restore:')) continue;
    const ast = parse(code), functions = new Map(ast.statements.filter(ts.isFunctionDeclaration).filter(n => n.name).map(n => [n.name.text, n]));
    for (const node of functions.values()) {
      const statements = node.body?.statements;
      if (node.parameters.length !== 0 || statements?.length !== 1 || !ts.isReturnStatement(statements[0])) continue;
      const expression = statements[0].expression;
      if (!expression || !ts.isBinaryExpression(expression) || expression.operatorToken.kind !== ts.SyntaxKind.QuestionQuestionEqualsToken || !ts.isIdentifier(expression.left) || !ts.isCallExpression(expression.right) || expression.right.arguments.length !== 0 || !ts.isIdentifier(expression.right.expression)) continue;
      const factory = functions.get(expression.right.expression.text), returns = factory?.body?.statements.filter(ts.isReturnStatement);
      if (returns?.length !== 1 || !returns[0].expression || !ts.isObjectLiteralExpression(returns[0].expression)) continue;
      const keys = returns[0].expression.properties.map(p => p.name?.getText(ast));
      if (!['state', 'personalContext', 'restore', 'login', 'logout', 'leave', 'restart', 'projects'].every(key => keys.includes(key))) continue;
      const exports = ast.statements.filter(ts.isExportDeclaration).flatMap(n => n.exportClause && ts.isNamedExports(n.exportClause) ? [...n.exportClause.elements] : []).filter(n => (n.propertyName ?? n.name).text === node.name.text);
      need(exports.length === 1, 'SESSION_PROBE_SINGLETON_EXPORT');
      matches.push({ asset: '/assets/' + file, export_name: exports[0].name.text });
    }
  }
  need(matches.length === 1, 'SESSION_PROBE_SINGLETON_UNIQUE');
  const html = await readFile(join(dist, 'index.html'), 'utf8');
  const scripts = [...html.matchAll(/<script\b[^>]*\bsrc="(\/assets\/[^"/]+\.js)"[^>]*>/g)];
  need(scripts.length === 1, 'SESSION_PROBE_APP_ENTRY');
  const entry = scripts[0][1], ast = parse(await readFile(join(dist, entry.slice(1)), 'utf8'));
  const imports = ast.statements.filter(ts.isImportDeclaration).map(n => n.moduleSpecifier.text);
  need(imports.filter(path => path === './' + basename(matches[0].asset)).length === 1, 'SESSION_PROBE_SINGLETON_LOADED');
  return { ...matches[0], entry };
}

// Resolve diagnostics need the exact publicly exported production failure type.
// Inspect the same already-loaded module; never hard-code a minified identifier.
export async function resolveOwnerModule(root) {
  const binding = await loginOwnerModule(root);
  const ts = createRequire(join(root, 'web/package.json'))('typescript');
  const code = await readFile(join(root, 'output/ai/model-ui-recovery/dist', binding.asset.slice(1)), 'utf8');
  const ast = ts.createSourceFile('asset.js', code, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS);
  const classes = [];
  for (const statement of ast.statements) {
    if (ts.isClassDeclaration(statement) && statement.name) classes.push({ name: statement.name.text, node: statement });
    if (ts.isVariableStatement(statement)) for (const declaration of statement.declarationList.declarations) {
      if (ts.isIdentifier(declaration.name) && declaration.initializer && ts.isClassExpression(declaration.initializer)) classes.push({ name: declaration.name.text, node: declaration.initializer });
    }
  }
  const matches = classes.filter(({ node }) => {
    if (!node.heritageClauses?.some(clause => clause.token === ts.SyntaxKind.ExtendsKeyword && clause.types.length === 1 && clause.types[0].expression.getText(ast) === 'Error')) return false;
    const constructor = node.members.find(ts.isConstructorDeclaration);
    if (!constructor || constructor.parameters.length !== 2 || !constructor.parameters.every(p => ts.isIdentifier(p.name))) return false;
    const assigned = new Map();
    const visit = (child) => {
      if (ts.isBinaryExpression(child) && child.operatorToken.kind === ts.SyntaxKind.EqualsToken && ts.isPropertyAccessExpression(child.left) && child.left.expression.kind === ts.SyntaxKind.ThisKeyword) assigned.set(child.left.name.text, child.right);
      ts.forEachChild(child, visit);
    };
    visit(constructor);
    return assigned.get('kind')?.getText(ast) === constructor.parameters[0].name.text && assigned.get('problem')?.getText(ast) === constructor.parameters[1].name.text && assigned.has('name') && ts.isStringLiteralLike(assigned.get('name')) && assigned.get('name').text === 'AccountFailure';
  });
  need(matches.length === 1, 'RESOLVE_PROBE_FAILURE_TYPE_UNIQUE');
  const exports = ast.statements.filter(ts.isExportDeclaration).flatMap(n => n.exportClause && ts.isNamedExports(n.exportClause) ? [...n.exportClause.elements] : []).filter(n => (n.propertyName ?? n.name).text === matches[0].name);
  need(exports.length === 1, 'RESOLVE_PROBE_FAILURE_TYPE_EXPORT');
  return { ...binding, failure_export_name: exports[0].name.text };
}

