#!/usr/bin/env python3
"""Read-only Knowledge HTTP root input and consumer binding controls."""
import ast
from pathlib import Path
import subprocess
import sys

ROOT = Path('/workspace/agenteam-knowledge-http')
REL = '.agent-state/work-owner-http/root_chain_driver.py'
source = (ROOT / REL).read_text()
old = subprocess.check_output(['git', 'show', 'bb98b6cd:' + REL], cwd=ROOT, text=True)


def load(text):
    env = {'__file__': str(ROOT / REL), '__name__': 'review_only'}
    exec(compile(text, str(ROOT / REL), 'exec'), env)
    return env


current, before = load(source), load(old)
binary = ROOT / 'output/ai/knowledge-owner-read/knowledge-owner-read-http-race.test'
added = {Path(sys.executable).resolve(), ROOT / '.agent-state/knowledge-owner-read/schema-controls.py',
         ROOT / 'api/openapi/knowledge-owner.json', ROOT / 'api/openapi/common.json'}
old_inputs = before['input_paths'](binary)
new_inputs = current['input_paths'](binary)
assert len(new_inputs) == len(set(new_inputs))
assert set(new_inputs) - set(old_inputs) == added
assert set(old_inputs) - set(new_inputs) == set()
assert all(path.is_file() for path in added)
selector = '^TestKnowledgeOwnerReadHTTP(Metadata|CurrentAuthority|Transactions|CommitUnknown)$'
assert {k: v for k, v in current['TARGETS'].items() if k != selector} == before['TARGETS']
assert current['TARGETS'][selector] == 'tests/knowledge'

# Execute only the real env.update AST, never main or execve. A hostile
# inherited value must be replaced by the interpreter whose path is frozen.
main = next(node for node in ast.parse(source).body if isinstance(node, ast.FunctionDef) and node.name == 'main')
updates = [node for node in main.body if isinstance(node, ast.Expr)
           and isinstance(node.value, ast.Call) and isinstance(node.value.func, ast.Attribute)
           and isinstance(node.value.func.value, ast.Name)
           and node.value.func.value.id == 'env' and node.value.func.attr == 'update']
assert len(updates) == 1
env = dict(current, env={'AGENTEAM_KNOWLEDGE_HTTP_SCHEMA_PYTHON': '/not-the-frozen-python'},
           plan={'binary': str(binary), 'cwd': str(ROOT / 'tests/knowledge')},
           directory=Path('/not-created/owned'), runtime=Path('/not-created/owned/runtime'))
exec(compile(ast.Module(body=updates, type_ignores=[]), str(ROOT / REL), 'exec'), env)
interpreter = env['env']['AGENTEAM_KNOWLEDGE_HTTP_SCHEMA_PYTHON']
assert Path(interpreter) == current['SCHEMA_PYTHON'] == Path(sys.executable).resolve()
assert Path(interpreter) in new_inputs
assert env['env']['AGENTEAM_FIXTURE_TEST_CWD'] == str(ROOT / 'tests/knowledge')
assert env['env']['GOFLAGS'] == '-mod=readonly -p=2'

# Bind to the actual compiled Go consumer and the helper's local data reads.
consumer = (ROOT / 'tests/knowledge/owner_read_http_test.go').read_text()
assert 'os.Getenv("AGENTEAM_KNOWLEDGE_HTTP_SCHEMA_PYTHON")' in consumer
assert 'exec.CommandContext(ctx, python, script)' in consumer
assert 'filepath.Abs("../../.agent-state/knowledge-owner-read/schema-controls.py")' in consumer
helper = (ROOT / '.agent-state/knowledge-owner-read/schema-controls.py').read_text()
tree = ast.parse(helper)
loops = [node for node in tree.body if isinstance(node, ast.For)
         and isinstance(node.target, ast.Name) and node.target.id == 'name']
assert len(loops) == 1
assert ast.literal_eval(loops[0].iter) == ('knowledge-owner.json', 'common.json')
assert 'root / "api/openapi" / name' in helper

# Local mutations demonstrate that each required runtime input and interpreter
# binding is independently necessary; no candidate/source file is modified.
def complete(paths, python):
    return added <= paths and Path(python) == current['SCHEMA_PYTHON']

assert complete(set(new_inputs), interpreter)
for removed in added:
    assert not complete(set(new_inputs) - {removed}, interpreter)
assert not complete(set(new_inputs), '/not-the-frozen-python')
print('PASS exact old inputs plus 4; same interpreter env and actual Go/helper consumers; 5 omission/substitution negatives; no main/child/PG/socket')
