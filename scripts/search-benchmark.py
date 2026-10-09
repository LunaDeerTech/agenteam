#!/usr/bin/env python3
"""Validate and score fixed lexical evaluation data without running a backend."""

import argparse
from collections import Counter, defaultdict
import json
import math
import os
from pathlib import Path
import re
import stat
import sys


MAX_FILE = 2 * 1024 * 1024
MAX_LINE = 64 * 1024
CATEGORIES = ("zh", "en", "mixed", "identifier", "case_style", "path", "error_code", "architecture")
SPLITS = ("dev", "test")
BACKENDS = ("native_pg_fts", "pgroonga", "pg_search")
KS = (1, 5, 10, 20)
ID = re.compile(r"[a-z][a-z0-9_-]{0,63}\Z", re.ASCII)


class Invalid(ValueError):
    """A bounded input or output-contract violation, without input contents."""


def require(condition, code):
    if not condition:
        raise Invalid(code)


def fields(value, expected, code):
    require(type(value) is dict and set(value) == set(expected.split()), code)


def text(value, limit, code):
    require(type(value) is str and bool(value.strip()), code)
    try:
        encoded = value.encode("utf-8", errors="strict")
    except UnicodeError:
        raise Invalid(code) from None
    require(len(encoded) <= limit and "\x00" not in value and "\r" not in value, code)


def identifier(value):
    require(type(value) is str and ID.fullmatch(value) is not None, "invalid_id")


def integer(value, low, high, code):
    require(type(value) is int and low <= value <= high, code)


def finite(value, code, nonnegative=False):
    require(type(value) in (int, float), code)
    try:
        valid = math.isfinite(value)
    except OverflowError:
        valid = False
    require(valid and (not nonnegative or value >= 0), code)


def unique_members(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, "duplicate_json_member")
        value[key] = item
    return value


def reject_constant(_value):
    raise Invalid("nonfinite_json_constant")


def decode(raw):
    try:
        return json.loads(raw, object_pairs_hook=unique_members, parse_constant=reject_constant)
    except Invalid:
        raise
    except (ValueError, RecursionError):
        raise Invalid("invalid_json") from None


def read(path, lines=False):
    try:
        # A FIFO must not block at open before the regular-file check. The
        # repository's tooling environment is POSIX; no other I/O is attempted.
        descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK)
        with os.fdopen(descriptor, "rb") as source:
            require(stat.S_ISREG(os.fstat(source.fileno()).st_mode), "input_not_regular")
            raw = source.read(MAX_FILE + 1)
    except OSError:
        raise Invalid("input_unreadable") from None
    require(len(raw) <= MAX_FILE, "input_too_large")
    try:
        value = raw.decode("utf-8", errors="strict")
    except UnicodeError:
        raise Invalid("invalid_utf8") from None
    require(bool(value) and value.endswith("\n") and "\r" not in value and "\x00" not in value, "invalid_text_framing")
    require(all(len(line) <= MAX_LINE for line in raw.split(b"\n")), "line_too_large")
    if not lines:
        return decode(value)
    records = value[:-1].split("\n")
    require(all(record.strip() for record in records), "empty_jsonl_record")
    return [decode(record) for record in records]


def indexed(rows, key):
    require(type(rows) is list, "invalid_record_list")
    result = {}
    for row in rows:
        require(type(row) is dict and key in row, "invalid_record")
        identifier(row[key])
        require(row[key] not in result, "duplicate_id")
        result[row[key]] = row
    return result


def load_dataset(directory):
    base = Path(directory)
    meta = read(base / "dataset.json")
    fields(meta, "format_version dataset_revision documents sources answerable_queries no_answer_queries categories splits source_notice authorship judgment_method", "invalid_dataset_fields")
    integer(meta["format_version"], 1, 1, "invalid_format_version")
    identifier(meta["dataset_revision"])
    for name, count in (("documents", 32), ("sources", 64), ("answerable_queries", 64), ("no_answer_queries", 8)):
        integer(meta[name], count, count, "invalid_dataset_count")
    require(meta["categories"] == list(CATEGORIES) and meta["splits"] == list(SPLITS), "invalid_dataset_groups")
    for name in ("source_notice", "authorship", "judgment_method"):
        text(meta[name], 2048, "invalid_source_statement")
    revision = meta["dataset_revision"]
    corpus = indexed(read(base / "corpus.jsonl", lines=True), "source_id")
    queries = indexed(read(base / "queries.jsonl", lines=True), "query_id")
    qrels = indexed(read(base / "qrels.jsonl", lines=True), "query_id")
    require(len(corpus) == 64 and len(queries) == 72 and set(queries) == set(qrels), "invalid_dataset_members")
    documents = defaultdict(list)
    for row in corpus.values():
        fields(row, "dataset_revision document_id source_id title section raw_text", "invalid_corpus_fields")
        require(row["dataset_revision"] == revision, "dataset_revision_mismatch")
        identifier(row["document_id"])
        for name in ("title", "section"):
            text(row[name], 160, "invalid_heading")
            require("\n" not in row[name], "invalid_heading")
        text(row["raw_text"], 4096, "invalid_raw_text")
        require(len(row["raw_text"].encode("utf-8")) >= 128, "raw_text_too_short")
        documents[row["document_id"]].append(row)
    require(len(documents) == 32, "invalid_document_count")
    for rows in documents.values():
        require(len(rows) == 2 and len({r["title"] for r in rows}) == 1 and len({r["section"] for r in rows}) == 2, "invalid_document_sections")

    families = defaultdict(set)
    counts = Counter()
    multiple_relevant = 0
    reasoned_negatives = 0
    for qid, query in queries.items():
        fields(query, "dataset_revision query_id family_id split category answerable text", "invalid_query_fields")
        require(query["dataset_revision"] == revision, "dataset_revision_mismatch")
        identifier(query["family_id"])
        require(type(query["answerable"]) is bool and query["split"] in SPLITS, "invalid_query_group")
        require(query["category"] in (CATEGORIES if query["answerable"] else ("no_answer",)), "invalid_query_category")
        text(query["text"], 256, "invalid_query_text")
        families[query["family_id"]].add(query["split"])
        counts[(query["category"], query["split"])] += 1
        rel = qrels[qid]
        fields(rel, "dataset_revision query_id intent grades reasons", "invalid_qrels_fields")
        require(rel["dataset_revision"] == revision, "dataset_revision_mismatch")
        text(rel["intent"], 2048, "invalid_query_intent")
        require(type(rel["grades"]) is dict and set(rel["grades"]) == set(corpus), "incomplete_qrels")
        require(type(rel["reasons"]) is dict and set(rel["reasons"]) <= set(corpus), "invalid_reason_source")
        for sid, grade in rel["grades"].items():
            integer(grade, 0, 3, "invalid_grade")
            require(grade == 0 or sid in rel["reasons"], "missing_relevance_reason")
        for reason in rel["reasons"].values():
            text(reason, 2048, "invalid_relevance_reason")
        positive_count = sum(grade >= 2 for grade in rel["grades"].values())
        if query["answerable"]:
            require(positive_count > 0, "answerable_without_positive")
            multiple_relevant += positive_count >= 2
            reasoned_negatives += any(rel["grades"][sid] == 0 for sid in rel["reasons"])
        else:
            require(all(grade == 0 for grade in rel["grades"].values()), "no_answer_has_relevance")
    require(all(len(splits) == 1 for splits in families.values()), "family_crosses_split")
    require(counts == Counter({(category, split): 4 for category in (*CATEGORIES, "no_answer") for split in SPLITS}), "unbalanced_query_groups")
    require(multiple_relevant >= 16 and reasoned_negatives >= 8, "insufficient_judgment_coverage")
    return {"meta": meta, "corpus": corpus, "queries": queries, "qrels": qrels}


def load_run(path, dataset):
    run = read(path)
    fields(run, "format_version dataset_revision backend backend_version config results measurements", "invalid_run_fields")
    integer(run["format_version"], 1, 1, "invalid_format_version")
    require(run["dataset_revision"] == dataset["meta"]["dataset_revision"], "dataset_revision_mismatch")
    require(run["backend"] in BACKENDS, "invalid_backend")
    text(run["backend_version"], 512, "invalid_backend_version")
    config = run["config"]
    fields(config, "analyzer query_mode ranking candidate_k tie_break adapter_revision", "invalid_config_fields")
    integer(config["candidate_k"], 20, 20, "invalid_candidate_k")
    require(config["tie_break"] == "source_id_ascii", "invalid_tie_break")
    for name in ("analyzer", "query_mode", "ranking", "adapter_revision"):
        text(config[name], 512, "invalid_config_value")
    results = indexed(run["results"], "query_id")
    require(set(results) == set(dataset["queries"]), "incomplete_run")
    for result in results.values():
        fields(result, "query_id hits", "invalid_result_fields")
        hits = result["hits"]
        require(type(hits) is list and len(hits) <= 20, "invalid_hit_count")
        seen = set()
        previous = None
        for rank, hit in enumerate(hits, 1):
            fields(hit, "source_id rank native_score", "invalid_hit_fields")
            integer(hit["rank"], rank, rank, "nonconsecutive_rank")
            identifier(hit["source_id"])
            require(hit["source_id"] in dataset["corpus"] and hit["source_id"] not in seen, "invalid_hit_source")
            seen.add(hit["source_id"])
            if hit["native_score"] is not None:
                finite(hit["native_score"], "invalid_native_score")
                if previous is not None and previous["native_score"] == hit["native_score"]:
                    require(previous["source_id"] < hit["source_id"], "unstable_score_tie")
            previous = hit
    validate_measurements(run["measurements"], dataset)
    return run, results


def validate_measurements(value, dataset):
    if value is None:
        return
    fields(value, "index_bytes indexing_ms environment latencies", "invalid_measurement_fields")
    if value["index_bytes"] is not None:
        require(type(value["index_bytes"]) is int and value["index_bytes"] >= 0, "invalid_index_bytes")
    if value["indexing_ms"] is not None:
        finite(value["indexing_ms"], "invalid_indexing_ms", nonnegative=True)
    text(value["environment"], 1024, "invalid_measurement_environment")
    samples = value["latencies"]
    require(type(samples) is list and len(samples) <= 144, "invalid_latency_count")
    seen = set()
    for row in samples:
        fields(row, "query_id mode samples_ms", "invalid_latency_fields")
        identifier(row["query_id"])
        require(row["query_id"] in dataset["queries"] and row["mode"] in ("cold", "warm"), "invalid_latency_identity")
        identity = (row["query_id"], row["mode"])
        require(identity not in seen, "duplicate_latency_identity")
        seen.add(identity)
        require(type(row["samples_ms"]) is list and 1 <= len(row["samples_ms"]) <= 100, "invalid_latency_samples")
        for sample in row["samples_ms"]:
            finite(sample, "invalid_latency_value", nonnegative=True)


def metrics(source_ids, grades):
    """Ranks are one-based; grades 2/3 count for binary relevance."""
    relevant = sum(grade >= 2 for grade in grades.values())
    require(relevant > 0, "metric_without_positive")
    ranked = [grades[sid] for sid in source_ids[:20]]
    ideal = sorted(grades.values(), reverse=True)
    result = {"mrr_at_20": next((1 / rank for rank, grade in enumerate(ranked, 1) if grade >= 2), 0.0)}
    for k in KS:
        result[f"recall_at_{k}"] = sum(grade >= 2 for grade in ranked[:k]) / relevant
        dcg = math.fsum((2**grade - 1) / math.log2(rank + 1) for rank, grade in enumerate(ranked[:k], 1))
        idcg = math.fsum((2**grade - 1) / math.log2(rank + 1) for rank, grade in enumerate(ideal[:k], 1))
        result[f"ndcg_at_{k}"] = dcg / idcg
    return result


def aggregate(rows):
    require(bool(rows), "empty_metric_group")
    return {"queries": len(rows), **{key: math.fsum(row[key] for row in rows) / len(rows) for key in rows[0]}}


def score(dataset, run, results):
    per_query = []
    groups = defaultdict(list)
    no_answer = []
    for qid, query in sorted(dataset["queries"].items()):
        hits = results[qid]["hits"]
        row = {"query_id": qid, "split": query["split"], "category": query["category"], "answerable": query["answerable"], "returned_candidates": len(hits), "metrics": None}
        if query["answerable"]:
            row["metrics"] = metrics([hit["source_id"] for hit in hits], dataset["qrels"][qid]["grades"])
            for name in ("overall", "split:" + query["split"], "category:" + query["category"], "category_split:" + query["category"] + ":" + query["split"]):
                groups[name].append(row["metrics"])
        else:
            no_answer.append(row)
        per_query.append(row)
    return {"format_version": 1, "dataset_revision": run["dataset_revision"], "provenance": "imported_run", "backend": run["backend"], "backend_version": run["backend_version"], "config": run["config"], "groups": {key: aggregate(rows) for key, rows in sorted(groups.items())}, "no_answer": {"queries": len(no_answer), "queries_with_hits": sum(row["returned_candidates"] > 0 for row in no_answer), "fraction_with_hits": sum(row["returned_candidates"] > 0 for row in no_answer) / len(no_answer), "returned_candidates": sum(row["returned_candidates"] for row in no_answer)}, "per_query": per_query}


def percentiles(samples):
    ordered = sorted(samples)
    return {"samples": len(ordered), "p50_ms": ordered[(50 * len(ordered) + 99) // 100 - 1], "p95_ms": ordered[(95 * len(ordered) + 99) // 100 - 1]}


def measurement_report(value):
    if value is None:
        return {"provenance": "not_measured", "index_bytes": None, "indexing_ms": None, "environment": None, "latencies": [], "by_mode": {}}
    modes = defaultdict(list)
    latencies = []
    for row in sorted(value["latencies"], key=lambda item: (item["query_id"], item["mode"])):
        modes[row["mode"]].extend(row["samples_ms"])
        latencies.append({"query_id": row["query_id"], "mode": row["mode"], **percentiles(row["samples_ms"])})
    return {"provenance": "reported_measurements", "index_bytes": value["index_bytes"], "indexing_ms": value["indexing_ms"], "environment": value["environment"], "latencies": latencies, "by_mode": {mode: percentiles(samples) for mode, samples in sorted(modes.items())}}


def json_text(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2, allow_nan=False) + "\n"


def json_lines(rows):
    return "".join(json.dumps(row, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False) + "\n" for row in rows)


def publish(directory, outputs, kind):
    """Only an exclusively-created output is owned; completion is written last."""
    target = Path(directory)
    try:
        target.mkdir()
    except OSError:
        raise Invalid("output_must_be_new_directory") from None
    owned = []
    try:
        for name, contents in outputs.items():
            path = target / name
            with path.open("x", encoding="utf-8", newline="\n") as stream:
                owned.append(path)
                stream.write(contents)
        marker = target / "complete.json"
        with marker.open("x", encoding="utf-8", newline="\n") as stream:
            owned.append(marker)
            stream.write(json_text({"format_version": 1, "kind": kind, "files": sorted(outputs)}))
    except BaseException:
        for path in reversed(owned):
            path.unlink(missing_ok=True)
        target.rmdir()
        raise


def export(dataset):
    revision = dataset["meta"]["dataset_revision"]
    return {"corpus.jsonl": json_lines({"dataset_revision": revision, "source_id": sid, "index_text": row["title"] + "\n" + row["section"] + "\n\n" + row["raw_text"]} for sid, row in sorted(dataset["corpus"].items())), "queries.jsonl": json_lines({"dataset_revision": revision, "query_id": qid, "text": row["text"]} for qid, row in sorted(dataset["queries"].items()))}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("validate", "export", "score"):
        command = commands.add_parser(name)
        command.add_argument("--data", required=True)
        if name != "validate":
            command.add_argument("--output", required=True)
        if name == "score":
            command.add_argument("--run", required=True)
    args = parser.parse_args(argv)
    try:
        dataset = load_dataset(args.data)
        if args.command == "export":
            publish(args.output, export(dataset), "export")
        elif args.command == "score":
            run, results = load_run(args.run, dataset)
            publish(args.output, {"report.json": json_text(score(dataset, run, results)), "measurements.json": json_text(measurement_report(run["measurements"]))}, "score")
        print(json_text({"status": "ok", "command": args.command, "dataset_revision": dataset["meta"]["dataset_revision"], "documents": 32, "sources": 64, "queries": 72, "judgments": 4608, "semantic_review": "not_established_by_validator"}), end="")
        return 0
    except (Invalid, OSError, UnicodeError, RecursionError, OverflowError) as error:
        print("search_benchmark_error=" + (str(error) if isinstance(error, Invalid) else "io_or_encoding_failure"), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
