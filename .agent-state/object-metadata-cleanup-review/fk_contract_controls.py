#!/usr/bin/env python3
"""Read-only D05 migration topology controls; not a SQL/PG execution test.

Usage: python3 fk_contract_controls.py /path/to/agenteam-object-metadata-cleanup
The abstract FK oracle checks the review's deletion order against constraints
read from the actual migrations. It neither imports nor copies a purge routine.
"""
from dataclasses import dataclass
from pathlib import Path
import re
import sys


@dataclass(frozen=True)
class Edge:
    child: str
    column: str
    parent: str
    deferred: bool


def main(root: Path) -> None:
    migration = root / "db/migrations"
    base = (migration / "00005_object_storage.sql").read_text()
    transfer = (migration / "00007_object_transfer.sql").read_text()

    def inline(source: str, table: str, column: str, parent: str) -> Edge:
        block = source.split(f"CREATE TABLE agenteam_object.{table} (", 1)[1].split("\n);", 1)[0]
        line = next(line.strip() for line in block.splitlines() if line.strip().startswith(column + " "))
        assert f"REFERENCES agenteam_object.{parent}(id)" in line, line
        assert "CASCADE" not in line, line
        return Edge(table, column, parent, "DEFERRABLE INITIALLY DEFERRED" in line)

    edges = [
        inline(base, "uploads", "object_id", "objects"),
        inline(base, "upload_attempts", "upload_id", "uploads"),
        inline(base, "upload_attempts", "object_id", "objects"),
        inline(base, "cleanup_operations", "attempt_id", "upload_attempts"),
        inline(base, "cleanup_operations", "object_id", "objects"),
        inline(base, "object_leases", "attempt_id", "upload_attempts"),
        inline(base, "object_leases", "object_id", "objects"),
        inline(transfer, "object_transfers", "object_id", "objects"),
        inline(transfer, "object_transfers", "upload_id", "uploads"),
        inline(transfer, "object_transfers", "staging_id", "upload_attempts"),
        inline(transfer, "object_transfers", "candidate_id", "upload_attempts"),
        inline(transfer, "object_transfers", "lease_id", "object_leases"),
        inline(transfer, "object_transfers", "source_lease_id", "object_leases"),
    ]
    for source, child, column, parent, deferred in (
        (base, "uploads", "current_attempt_id", "upload_attempts", False),
        (transfer, "upload_attempts", "transfer_id", "object_transfers", True),
    ):
        line = next(line for line in source.splitlines() if line.startswith(f"ALTER TABLE agenteam_object.{child} ADD CONSTRAINT") and f"FOREIGN KEY({column})" in line)
        assert f"REFERENCES agenteam_object.{parent}(id)" in line
        assert ("DEFERRABLE INITIALLY DEFERRED" in line) == deferred
        assert "CASCADE" not in line
        edges.append(Edge(child, column, parent, deferred))

    initial = {
        ("objects", "o"): {},
        ("uploads", "u"): {"object_id": "o", "current_attempt_id": "a"},
        ("upload_attempts", "a"): {"object_id": "o", "upload_id": "u"},
        ("upload_attempts", "b"): {"object_id": "o", "upload_id": "u"},
        ("upload_attempts", "s"): {"object_id": "o", "upload_id": "u", "transfer_id": "t"},
        ("cleanup_operations", "ca"): {"object_id": "o", "attempt_id": "a"},
        ("cleanup_operations", "cb"): {"object_id": "o", "attempt_id": "b"},
        ("cleanup_operations", "cs"): {"object_id": "o", "attempt_id": "s"},
        ("object_leases", "writer"): {"object_id": "o", "attempt_id": "b"},
        ("object_leases", "external"): {"object_id": "o"},
        ("object_leases", "source"): {"object_id": "o"},
        ("object_transfers", "t"): {"object_id": "o", "upload_id": "u", "staging_id": "s", "candidate_id": "b", "lease_id": "external", "source_lease_id": "source"},
    }

    def valid(rows: dict, commit: bool) -> bool:
        return all(
            edge.deferred and not commit or values.get(edge.column) is None or (edge.parent, values[edge.column]) in rows
            for (table, _), values in rows.items()
            for edge in edges if edge.child == table
        )

    def tx(start: dict, deletes: list, clear_current: bool = False) -> dict | None:
        rows = {key: dict(value) for key, value in start.items()}
        if clear_current:
            rows[("uploads", "u")]["current_attempt_id"] = None
        for key in deletes:
            del rows[key]
            if not valid(rows, commit=False):
                return None
        return rows if valid(rows, commit=True) else None

    assert valid(initial, commit=True)
    # The two deferred cycle edges allow the whole package, not either half.
    package = [("cleanup_operations", "cs"), ("upload_attempts", "s"), ("object_transfers", "t")]
    after_package = tx(initial, package)
    assert after_package is not None
    assert tx(initial, package[:2]) is None
    assert tx(initial, [("object_transfers", "t")]) is None
    assert tx(initial, [("cleanup_operations", "cb"), ("upload_attempts", "b")]) is None
    assert tx(initial, [("object_leases", "source")]) is None
    assert tx(initial, [("object_leases", "external")]) is None
    # Immediate incoming dependencies still apply even inside the PUT package.
    foreign = {key: dict(value) for key, value in initial.items()}
    foreign[("object_leases", "unexpected")] = {"object_id": "o", "attempt_id": "s"}
    assert tx(foreign, package) is None
    after_leases = tx(after_package, [("object_leases", name) for name in ("writer", "external", "source")])
    assert after_leases is not None
    after_history = tx(after_leases, [("cleanup_operations", "cb"), ("upload_attempts", "b")])
    assert after_history is not None
    anchors = [("cleanup_operations", "ca"), ("upload_attempts", "a"), ("uploads", "u"), ("objects", "o")]
    assert tx(after_history, anchors) is None
    assert tx(after_history, anchors, clear_current=True) == {}

    # Source-backed implementation obligations, not measured planner costs.
    all_sql = "\n".join(path.read_text() for path in sorted(migration.glob("*.sql")))
    for table, column in (("uploads", "current_attempt_id"), ("upload_attempts", "transfer_id")):
        create = rf"CREATE\s+(?:UNIQUE\s+)?INDEX\s+\S+\s+ON\s+agenteam_object\.{table}\s*\(\s*{column}\b"
        assert re.search(create, all_sql, re.I) is None
        assert re.search(rf"UNIQUE\s*\(\s*{column}\b", all_sql, re.I) is None
    print("PASS: 15 actual FK edges; whole PUT package and final anchors; seven rejected partial/dependency orders; two unindexed incoming edges. Static model only, no SQL/PG/performance execution.")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: fk_contract_controls.py REPOSITORY")
    main(Path(sys.argv[1]).resolve(strict=True))
