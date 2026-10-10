#!/usr/bin/env python3
"""Run the two independent Session scenarios with the existing PG supervisor.

No timeout, process, resource, TCP or original case logic is replaced. This
adapter only registers one exact selector and captures its own source as input.
"""
from pathlib import Path
import hashlib
import sys

ROOT = Path(__file__).resolve().parents[2]
SUPERVISOR = ROOT / ".agent-state/task-planning-recovery/pg_only_supervisor.py"
SELECTOR = "^TestSkillOwnerReadHTTPIndependentCurrentSession$"
CASES = {"TestSkillOwnerReadHTTPIndependentCurrentSession": ("get_detail", "head_detail")}


def load_supervisor():
    namespace = {"__file__": str(SUPERVISOR), "__name__": "skills_http_independent"}
    exec(compile(SUPERVISOR.read_text(), str(SUPERVISOR), "exec"), namespace)
    namespace["SKILL_HTTP_CASES"][SELECTOR] = CASES
    namespace["SKILL_HTTP_PG"] = SELECTOR
    original_inputs = namespace["skill_http_inputs"]

    def inputs(driver, binary, selector):
        if selector != SELECTOR:
            raise ValueError("exact independent Skill HTTP selector required")
        result = original_inputs(driver, binary, selector)
        required = ROOT / "tests/skills/owner_http_independent_test.go"
        result[str(required)] = hashlib.sha256(required.read_bytes()).hexdigest()
        own = Path(__file__).resolve()
        result[str(own)] = hashlib.sha256(own.read_bytes()).hexdigest()
        return result

    namespace["skill_http_inputs"] = inputs
    return namespace


def main():
    if sys.argv.count("--run") != 1:
        raise SystemExit("one exact --run selector required")
    index = sys.argv.index("--run")
    if index + 1 >= len(sys.argv) or sys.argv[index + 1] != SELECTOR:
        raise SystemExit("exact independent Skill HTTP selector required")
    return load_supervisor()["main"]()


if __name__ == "__main__":
    sys.exit(main())
