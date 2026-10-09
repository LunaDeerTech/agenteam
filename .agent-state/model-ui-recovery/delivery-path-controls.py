#!/usr/bin/env python3
"""Check delivery rebinding without importing a driver or creating resources."""
import ast
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BASELINE = "96934da4"
PATHS = (
    ".agent-state/model-ui-recovery/run-owned-top.py",
    ".agent-state/model-ui-recovery/fixture-go.py",
    ".agent-state/model-ui-regression/run-owned-regression.py",
    ".agent-state/model-ui-independent/run-independent.py",
)


def admit(source, original):
    old = 'DELIVERY = Path("/workspace/agenteam-delivery")'
    new = "DELIVERY = ROOT"
    if original.count(old) != 1 or source.count(new) != 1:
        return False
    if source.replace(new, old) != original:
        return False
    tree = ast.parse(source)
    delivery = [node.value for node in tree.body if isinstance(node, ast.Assign)
                and any(isinstance(target, ast.Name) and target.id == "DELIVERY"
                        for target in node.targets)]
    return len(delivery) == 1 and isinstance(delivery[0], ast.Name) and delivery[0].id == "ROOT"


for name in PATHS:
    source = (ROOT / name).read_text()
    original = subprocess.check_output(
        ["git", "show", BASELINE + ":" + name], cwd=ROOT, text=True)
    assert admit(source, original), name
    assert not admit(original, original), name
    assert not admit(source.replace("DELIVERY = ROOT", 'DELIVERY = Path("/other/tree")'), original), name
    assert not admit(source + "\nDELIVERY = ROOT\n", original), name
    assert not admit(source + "\n# unrelated source change\n", original), name
    assert not admit(source.replace("DELIVERY = ROOT", "DELIVERY = ROOT.parent"), original), name
    assert (ROOT / name).resolve().parents[2] == ROOT

print("4 actual driver bindings accepted; 20 negative controls rejected; original selectors, budgets and gate source unchanged; no driver imported")
