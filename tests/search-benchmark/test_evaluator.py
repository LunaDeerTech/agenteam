"""Evaluator controls only; generated runs are never backend evidence."""

import copy
import importlib.util
import json
import math
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / "tests/search-benchmark/data"
SCRIPT = ROOT / "scripts/search-benchmark.py"
SPEC = importlib.util.spec_from_file_location("lexical_evaluator", SCRIPT)
EVAL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EVAL)


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, allow_nan=False) + "\n", encoding="utf-8")


def dump_lines(path, rows):
    path.write_text("".join(json.dumps(row, ensure_ascii=False, allow_nan=False) + "\n" for row in rows), encoding="utf-8")


def control_run(dataset):
    return {
        "format_version": 1,
        "dataset_revision": "lexical-v1",
        "backend": "native_pg_fts",
        "backend_version": "evaluator-control-not-a-backend-run",
        "config": {"analyzer": "control-only", "query_mode": "control-only", "ranking": "hand-written-control", "candidate_k": 20, "tie_break": "source_id_ascii", "adapter_revision": "evaluator-test"},
        "results": [{"query_id": qid, "hits": []} for qid in sorted(dataset["queries"])],
        "measurements": None,
    }


class EvaluatorTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.dataset = EVAL.load_dataset(DATA)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="lexical-control-")
        self.base = Path(self.temp.name)
        self.addCleanup(self.temp.cleanup)

    def run_file(self, run):
        path = self.base / "control-run.json"
        dump(path, run)
        return path

    def cli(self, *args):
        return subprocess.run([sys.executable, str(SCRIPT), *map(str, args)], cwd=ROOT, capture_output=True, text=True, timeout=10)

    def reject_run(self, run, code):
        with self.assertRaisesRegex(EVAL.Invalid, "^" + code + "$"):
            EVAL.load_run(self.run_file(run), self.dataset)

    def copied_data(self):
        target = self.base / "data"
        shutil.copytree(DATA, target)
        return target

    def test_fixed_data_shape_and_full_matrix(self):
        d = self.dataset
        self.assertEqual(len(d["corpus"]), 64)
        self.assertEqual(len(d["queries"]), 72)
        self.assertEqual(sum(len(r["grades"]) for r in d["qrels"].values()), 4608)
        self.assertEqual(sum(q["answerable"] for q in d["queries"].values()), 64)
        # This checks a declared family merge, not the semantic correctness of it.
        self.assertEqual(d["queries"]["q09"]["family_id"], d["queries"]["q37"]["family_id"])

    def test_hand_worked_graded_ranking(self):
        # Grades A=3, B=2, C=1, D=0. Ranking C,B,A,D:
        # DCG@5 = 1 + 3/log2(3) + 7/2; ideal = 7 + 3/log2(3) + 1/2.
        actual = EVAL.metrics(["c", "b", "a", "d"], {"a": 3, "b": 2, "c": 1, "d": 0})
        expected_ndcg = (1 + 3 / math.log2(3) + 3.5) / (7 + 3 / math.log2(3) + 0.5)
        self.assertEqual(actual["recall_at_1"], 0)
        self.assertEqual(actual["recall_at_5"], 1)
        self.assertEqual(actual["mrr_at_20"], 0.5)
        self.assertAlmostEqual(actual["ndcg_at_1"], 1 / 7, places=14)
        self.assertAlmostEqual(actual["ndcg_at_5"], expected_ndcg, places=14)
        self.assertEqual(actual["ndcg_at_20"], actual["ndcg_at_5"])

    def test_hand_worked_missed_relevant_source_in_denominator(self):
        actual = EVAL.metrics(["a", "c"], {"a": 3, "b": 2, "c": 0})
        self.assertEqual(actual["recall_at_20"], 0.5)
        self.assertEqual(actual["mrr_at_20"], 1)
        self.assertAlmostEqual(actual["ndcg_at_20"], 7 / (7 + 3 / math.log2(3)), places=14)

    def test_empty_rankings_are_zero_and_rank_21_is_excluded(self):
        grades = {f"n{i}": 0 for i in range(20)} | {"relevant": 3}
        self.assertTrue(all(v == 0 for v in EVAL.metrics([], grades).values()))
        actual = EVAL.metrics([*(f"n{i}" for i in range(20)), "relevant"], grades)
        self.assertTrue(all(v == 0 for v in actual.values()))
        with self.assertRaisesRegex(EVAL.Invalid, "metric_without_positive"):
            EVAL.metrics([], {"none": 0})

    def test_actual_cli_macro_empty_and_no_answer_separation(self):
        run = control_run(self.dataset)
        # q01 grades are explicitly asserted; expected values are not generated
        # by sorting qrels or by a reference search engine.
        self.assertEqual([self.dataset["qrels"]["q01"]["grades"][s] for s in ("d02-s1", "d01-s1", "d01-s2")], [0, 2, 2])
        run["results"][0]["hits"] = [
            {"source_id": "d02-s1", "rank": 1, "native_score": 3},
            {"source_id": "d01-s1", "rank": 2, "native_score": 2},
            {"source_id": "d01-s2", "rank": 3, "native_score": 1},
        ]
        run["results"][64]["hits"] = [{"source_id": "d01-s1", "rank": 1, "native_score": None}]
        output = self.base / "score"
        result = self.cli("score", "--data", DATA, "--run", self.run_file(run), "--output", output)
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads((output / "report.json").read_text())
        overall = report["groups"]["overall"]
        self.assertEqual(overall["queries"], 64)
        self.assertEqual(overall["recall_at_5"], 1 / 64)
        self.assertEqual(overall["mrr_at_20"], 0.5 / 64)
        expected_ndcg = (3 / math.log2(3) + 1.5) / (3 + 3 / math.log2(3))
        self.assertAlmostEqual(overall["ndcg_at_5"], expected_ndcg / 64, places=14)
        self.assertEqual(report["groups"]["split:dev"]["queries"], 32)
        self.assertEqual(report["groups"]["category:zh"]["queries"], 8)
        self.assertEqual(report["groups"]["category_split:zh:dev"]["queries"], 4)
        self.assertEqual(report["no_answer"]["fraction_with_hits"], 1 / 8)
        self.assertIsNone(report["per_query"][64]["metrics"])
        self.assertEqual(report["provenance"], "imported_run")
        measurement = json.loads((output / "measurements.json").read_text())
        self.assertEqual(measurement["provenance"], "not_measured")
        self.assertIsNone(measurement["index_bytes"])
        self.assertTrue((output / "complete.json").is_file())

    def test_actual_cli_replay_is_byte_identical(self):
        run = control_run(self.dataset)
        path = self.run_file(run)
        first, second = self.base / "first", self.base / "second"
        for output in (first, second):
            result = self.cli("score", "--data", DATA, "--run", path, "--output", output)
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sorted(p.name for p in first.iterdir()), sorted(p.name for p in second.iterdir()))
        for path in first.iterdir():
            self.assertEqual(path.read_bytes(), (second / path.name).read_bytes())
        result = self.cli("score", "--data", DATA, "--run", self.run_file(run), "--output", first)
        self.assertEqual(result.returncode, 1)
        self.assertIn("output_must_be_new_directory", result.stderr)
        self.assertEqual((first / "report.json").read_bytes(), (second / "report.json").read_bytes())

    def test_actual_export_exact_fields_and_same_index_text(self):
        target = self.base / "export"
        result = self.cli("export", "--data", DATA, "--output", target)
        self.assertEqual(result.returncode, 0, result.stderr)
        corpus = [json.loads(line) for line in (target / "corpus.jsonl").read_text().splitlines()]
        queries = [json.loads(line) for line in (target / "queries.jsonl").read_text().splitlines()]
        self.assertEqual((len(corpus), len(queries)), (64, 72))
        for row in corpus:
            self.assertEqual(set(row), {"dataset_revision", "source_id", "index_text"})
            source = self.dataset["corpus"][row["source_id"]]
            self.assertEqual(row["index_text"], source["title"] + "\n" + source["section"] + "\n\n" + source["raw_text"])
        for row in queries:
            self.assertEqual(set(row), {"dataset_revision", "query_id", "text"})
        self.assertEqual(sorted(p.name for p in target.iterdir()), ["complete.json", "corpus.jsonl", "queries.jsonl"])

    def test_actual_cli_validation_and_failure_never_publish_success(self):
        result = self.cli("validate", "--data", DATA)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["semantic_review"], "not_established_by_validator")
        run = control_run(self.dataset)
        run["results"].pop()
        output = self.base / "bad-score"
        result = self.cli("score", "--data", DATA, "--run", self.run_file(run), "--output", output)
        self.assertEqual(result.returncode, 1)
        self.assertIn("incomplete_run", result.stderr)
        self.assertNotIn('"status": "ok"', result.stdout)
        self.assertFalse(output.exists())

    def test_wrong_version_unknown_backend_and_config(self):
        for field, value, code in (
            ("dataset_revision", "different", "dataset_revision_mismatch"),
            ("format_version", True, "invalid_format_version"),
            ("backend", "python-bm25", "invalid_backend"),
            ("backend_version", "", "invalid_backend_version"),
            ("extra", 1, "invalid_run_fields"),
        ):
            with self.subTest(field=field):
                run = control_run(self.dataset)
                run[field] = value
                self.reject_run(run, code)
        for field, value, code in (
            ("candidate_k", True, "invalid_candidate_k"),
            ("candidate_k", 21, "invalid_candidate_k"),
            ("tie_break", "random", "invalid_tie_break"),
            ("analyzer", "", "invalid_config_value"),
            ("extra", 0, "invalid_config_fields"),
        ):
            with self.subTest(config=field, value=value):
                run = control_run(self.dataset)
                run["config"][field] = value
                self.reject_run(run, code)

    def test_run_membership_rank_duplicates_and_tie_controls(self):
        def one():
            run = control_run(self.dataset)
            run["results"][0]["hits"] = [{"source_id": "d01-s1", "rank": 1, "native_score": None}]
            return run
        for field, value, code in (
            ("source_id", "missing", "invalid_hit_source"),
            ("rank", 0, "nonconsecutive_rank"),
            ("rank", True, "nonconsecutive_rank"),
            ("native_score", True, "invalid_native_score"),
            ("native_score", "1", "invalid_native_score"),
            ("unexpected", 0, "invalid_hit_fields"),
        ):
            with self.subTest(field=field):
                run = one()
                run["results"][0]["hits"][0][field] = value
                self.reject_run(run, code)
        run = one()
        run["results"][0]["hits"].append({"source_id": "d01-s1", "rank": 2, "native_score": None})
        self.reject_run(run, "invalid_hit_source")
        run = one()
        run["results"].append(copy.deepcopy(run["results"][0]))
        self.reject_run(run, "duplicate_id")
        run = one()
        run["results"][0]["query_id"] = "missing"
        self.reject_run(run, "incomplete_run")
        run = one()
        run["results"][0]["hits"] = [{"source_id": sid, "rank": n, "native_score": 1} for n, sid in enumerate(("d01-s2", "d01-s1"), 1)]
        self.reject_run(run, "unstable_score_tie")
        run["results"][0]["hits"].reverse()
        for n, hit in enumerate(run["results"][0]["hits"], 1):
            hit["rank"] = n
        EVAL.load_run(self.run_file(run), self.dataset)
        run["results"][0]["hits"] = [{"source_id": sid, "rank": n, "native_score": None} for n, sid in enumerate(list(self.dataset["corpus"])[:21], 1)]
        self.reject_run(run, "invalid_hit_count")

    def test_strict_json_utf8_eof_and_resource_limits(self):
        path = self.base / "raw.json"
        cases = (
            (b'{"a":1,"a":2}\n', "duplicate_json_member"),
            (b'{"x":NaN}\n', "nonfinite_json_constant"),
            (b'{"x":Infinity}\n', "nonfinite_json_constant"),
            (b'{} {}\n', "invalid_json"),
            (b'{}\xff\n', "invalid_utf8"),
            (b'{}', "invalid_text_framing"),
            (b'{}\r\n', "invalid_text_framing"),
            (b' ' * (EVAL.MAX_LINE + 1) + b'\n', "line_too_large"),
            (b' ' * (EVAL.MAX_FILE + 1), "input_too_large"),
        )
        for contents, code in cases:
            with self.subTest(code=code):
                path.write_bytes(contents)
                with self.assertRaisesRegex(EVAL.Invalid, code):
                    EVAL.read(path)
        path.write_bytes(b'{}\n\n')
        with self.assertRaisesRegex(EVAL.Invalid, "empty_jsonl_record"):
            EVAL.read(path, lines=True)
        for value in (float("inf"), float("nan"), 10**1000):
            with self.subTest(number=type(value).__name__):
                with self.assertRaisesRegex(EVAL.Invalid, "finite"):
                    EVAL.finite(value, "finite")
        with self.assertRaisesRegex(EVAL.Invalid, "surrogate"):
            EVAL.text("\ud800", 100, "surrogate")
        with self.assertRaisesRegex(EVAL.Invalid, "input_not_regular"):
            EVAL.read("/dev/null")

    def test_complete_qrels_and_grade_reasons(self):
        data = self.copied_data()
        original = list(self.dataset["qrels"].values())
        cases = ("missing", "bool", "range", "reason", "no-answer", "no-positive", "revision", "unknown")
        for case in cases:
            rows = copy.deepcopy(original)
            if case == "missing":
                rows[0]["grades"].pop("d01-s1")
            elif case == "bool":
                rows[0]["grades"]["d01-s1"] = True
            elif case == "range":
                rows[0]["grades"]["d01-s1"] = 4
            elif case == "reason":
                rows[0]["reasons"].pop("d01-s1")
            elif case == "no-answer":
                rows[64]["grades"]["d01-s1"] = 1
                rows[64]["reasons"]["d01-s1"] = "invalid control"
            elif case == "no-positive":
                rows[0]["grades"] = {sid: 0 for sid in rows[0]["grades"]}
            elif case == "revision":
                rows[0]["dataset_revision"] = "wrong"
            else:
                rows[0]["reasons"]["unknown"] = "invalid control"
            dump_lines(data / "qrels.jsonl", rows)
            with self.subTest(case=case), self.assertRaises(EVAL.Invalid):
                EVAL.load_dataset(data)

    def test_family_cross_split_balance_and_label_types(self):
        data = self.copied_data()
        original = list(self.dataset["queries"].values())
        for case in ("family", "balance", "boolean", "category", "text", "duplicate"):
            rows = copy.deepcopy(original)
            if case == "family":
                # Swapping same-category queries preserves balance but crosses
                # two whole-family boundaries. No text similarity heuristic.
                rows[0]["split"], rows[4]["split"] = rows[4]["split"], rows[0]["split"]
            elif case == "balance":
                for row in rows:
                    if row["family_id"] == "f01":
                        row["split"] = "test"
            elif case == "boolean":
                rows[0]["answerable"] = 1
            elif case == "category":
                rows[64]["category"] = "zh"
            elif case == "text":
                rows[0]["text"] = " "
            else:
                rows.append(copy.deepcopy(rows[0]))
            dump_lines(data / "queries.jsonl", rows)
            with self.subTest(case=case), self.assertRaises(EVAL.Invalid):
                EVAL.load_dataset(data)

    def test_corpus_identity_title_and_text_limits(self):
        data = self.copied_data()
        original = list(self.dataset["corpus"].values())
        for case in ("short", "long", "title", "section", "document", "nul", "unknown"):
            rows = copy.deepcopy(original)
            if case == "short": rows[0]["raw_text"] = "short"
            elif case == "long": rows[0]["raw_text"] = "中" * 1366
            elif case == "title": rows[0]["title"] = "different title"
            elif case == "section": rows[0]["section"] = rows[1]["section"]
            elif case == "document": rows[0]["document_id"] = "new-document"
            elif case == "nul": rows[0]["raw_text"] += "\x00"
            else: rows[0]["unexpected"] = 1
            dump_lines(data / "corpus.jsonl", rows)
            with self.subTest(case=case), self.assertRaises(EVAL.Invalid):
                EVAL.load_dataset(data)

    def test_reported_measurements_nearest_rank_and_mode_separation(self):
        run = control_run(self.dataset)
        run["measurements"] = {"index_bytes": 0, "indexing_ms": 0, "environment": "control-only, no backend", "latencies": [
            {"query_id": "q01", "mode": "cold", "samples_ms": [100, 200]},
            {"query_id": "q01", "mode": "warm", "samples_ms": list(range(1, 21))},
        ]}
        EVAL.load_run(self.run_file(run), self.dataset)
        report = EVAL.measurement_report(run["measurements"])
        self.assertEqual(report["provenance"], "reported_measurements")
        self.assertEqual(report["by_mode"]["warm"], {"samples": 20, "p50_ms": 10, "p95_ms": 19})
        self.assertEqual(report["by_mode"]["cold"], {"samples": 2, "p50_ms": 100, "p95_ms": 200})
        self.assertEqual(report["index_bytes"], 0)
        self.assertIsNone(EVAL.measurement_report(None)["index_bytes"])

    def test_measurement_invalid_numbers_missing_ids_and_repeats(self):
        base = {"index_bytes": None, "indexing_ms": None, "environment": "control", "latencies": [{"query_id": "q01", "mode": "warm", "samples_ms": [1]}]}
        for case in ("boolean", "negative", "unknown", "mode", "empty", "too-many", "duplicate", "extra", "fraction-bytes"):
            value = copy.deepcopy(base)
            if case == "boolean": value["index_bytes"] = True
            elif case == "fraction-bytes": value["index_bytes"] = 1.5
            elif case == "negative": value["indexing_ms"] = -1
            elif case == "unknown": value["latencies"][0]["query_id"] = "missing"
            elif case == "mode": value["latencies"][0]["mode"] = "mixed"
            elif case == "empty": value["latencies"][0]["samples_ms"] = []
            elif case == "too-many": value["latencies"][0]["samples_ms"] = [1] * 101
            elif case == "duplicate": value["latencies"] *= 2
            else: value["extra"] = 0
            run = control_run(self.dataset)
            run["measurements"] = value
            with self.subTest(case=case), self.assertRaises(EVAL.Invalid):
                EVAL.load_run(self.run_file(run), self.dataset)

    def test_failed_output_write_removes_only_owned_new_files(self):
        target = self.base / "incomplete"
        original = Path.open
        def fail_second(path, *args, **kwargs):
            if path.name == "second.json":
                raise OSError("controlled write failure")
            return original(path, *args, **kwargs)
        with patch.object(Path, "open", fail_second):
            with self.assertRaises(OSError):
                EVAL.publish(target, {"first.json": "{}\n", "second.json": "{}\n"}, "control")
        self.assertFalse(target.exists())
        existing = self.base / "owned-elsewhere"
        existing.mkdir()
        keep = existing / "keep"
        keep.write_text("unchanged")
        with self.assertRaisesRegex(EVAL.Invalid, "output_must_be_new_directory"):
            EVAL.publish(existing, {"first.json": "{}\n"}, "control")
        self.assertEqual(keep.read_text(), "unchanged")


if __name__ == "__main__":
    unittest.main()
