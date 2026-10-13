#!/usr/bin/env python3
"""Collect or check E01 reference-source facts; never calculate game coverage."""

import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parent
SUMMARY = "7cffbcdad5fa36f22acef6bd4b869ecad4ca3fd9"
DATA = "aacd2a457333b258d044a642f38270c8cddbc628"
README = "3d63d61bcb7421a0d8ddda656bc851cc178067dd"
OFFICIAL = "499680d659b7a0ccb2fa9a809c345c5baf2ace7d"
RAW = "https://raw.githubusercontent.com/misode/mcmeta/"
API = "https://api.github.com/repos/misode/mcmeta/"
URLS = {
    "mojang-version": f"https://piston-meta.mojang.com/v1/packages/{OFFICIAL}/1.21.1.json",
    "mojang-release": "https://www.minecraft.net/en-us/article/minecraft-java-edition-1-21-1",
    "mcmeta-version": f"{RAW}{SUMMARY}/version.json",
    "mcmeta-summary-tree": f"{API}git/trees/{SUMMARY}?recursive=1",
    "mcmeta-registries": f"{RAW}{SUMMARY}/registries/data.min.json",
    "mcmeta-data-tree": f"{API}git/trees/{DATA}?recursive=1",
    "mcmeta-method": f"{RAW}{README}/README.md",
    "wiki-version-revision": "https://minecraft.wiki/api.php?action=query&format=json&titles=Java_Edition_1.21.1&prop=revisions&rvprop=ids",
}


def digest(body):
    return hashlib.sha256(body).hexdigest()


def encode(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def decode(source):
    return json.loads(source["body"])


def collect():
    if (ROOT / "sources.json").exists():
        raise ValueError("sources.json exists; preserve the candidate before collecting a new revision")

    def fetch(pair):
        key, url = pair
        request = urllib.request.Request(url, headers={"User-Agent": "agenteam-e01-source-catalog/1"})
        with urllib.request.urlopen(request, timeout=30) as response:
            body = response.read(4_000_001)
            if len(body) > 4_000_000:
                raise ValueError(f"source exceeds small-text limit: {key}")
            return key, {
                "url": url,
                "bytes": len(body),
                "sha256": digest(body),
                "body": body.decode("utf-8"),
            }

    with ThreadPoolExecutor(max_workers=3) as pool:
        sources = dict(pool.map(fetch, URLS.items()))
    result = derive(sources)
    (ROOT / "sources.json").write_bytes(encode(sources))
    write_catalog(result)


def resource_id(category, name):
    # Catalog IDs identify source concepts, NOT weighted acceptance leaves.
    return f"mcj/1.21.1/{category}/minecraft:{name}"


def data_identity(path):
    if not path.startswith("data/minecraft/"):
        return f"source-file/{path}", "source_metadata", path
    tail = path.removeprefix("data/minecraft/")
    prefix = ""
    if tail.startswith("datapacks/"):
        pack, tail = tail.removeprefix("datapacks/").split("/", 1)
        if not tail.startswith("data/minecraft/"):
            return f"source-file/{path}", "source_metadata", path
        prefix = f"experiment/{pack}/"
        tail = tail.removeprefix("data/minecraft/")
    parts = tail.rsplit(".", 1)[0].split("/")
    category = parts.pop(0)
    if category == "tags":
        category = "tag/" + parts.pop(0)
        if category == "tag/worldgen":
            category += "/" + parts.pop(0)
    elif category == "worldgen":
        category += "/" + parts.pop(0)
    category = prefix + category
    name = "/".join(parts)
    if not name or not path.endswith((".json", ".nbt")):
        raise ValueError(f"unhandled data path: {path}")
    return resource_id(category, name), category, name


def derive(sources):
    if set(sources) != set(URLS):
        raise ValueError("missing or unknown source")
    for key, source in sources.items():
        body = source["body"].encode()
        if source["url"] != URLS[key] or source["bytes"] != len(body) or source["sha256"] != digest(body):
            raise ValueError(f"source identity or bytes changed: {key}")
    official = decode(sources["mojang-version"])
    if hashlib.sha1(sources["mojang-version"]["body"].encode()).hexdigest() != OFFICIAL:
        raise ValueError("Mojang version object does not match its official SHA-1")
    version = decode(sources["mcmeta-version"])
    if official["id"] != "1.21.1" or official["type"] != "release" or version["id"] != official["id"] or not version["stable"]:
        raise ValueError("source version does not identify the candidate stable release")
    if official["releaseTime"] != version["release_time"]:
        raise ValueError("official and extracted release times conflict")
    trees = {key: decode(sources[key]) for key in ("mcmeta-summary-tree", "mcmeta-data-tree")}
    for key, tree in trees.items():
        if tree["truncated"] or len({x["path"] for x in tree["tree"]}) != len(tree["tree"]):
            raise ValueError(f"truncated or duplicate-path source tree: {key}")
    raw = sources["mcmeta-registries"]["body"].encode()
    blob = next(x for x in trees["mcmeta-summary-tree"]["tree"] if x["path"] == "registries/data.min.json")
    if blob["size"] != len(raw) or blob["sha"] != hashlib.sha1(b"blob " + str(len(raw)).encode() + b"\0" + raw).hexdigest():
        raise ValueError("registry bytes do not match the pinned summary tree blob")
    registries = decode(sources["mcmeta-registries"])
    entries = {}

    def add(identifier, category, name, reference):
        entry = entries.setdefault(identifier, {"id": identifier, "category": category, "name": name, "sources": []})
        if reference in entry["sources"]:
            raise ValueError(f"duplicate source mapping: {reference}")
        entry["sources"].append(reference)

    for category, names in registries.items():
        if len(set(names)) != len(names):
            raise ValueError(f"duplicate registry entry: {category}")
        for index, name in enumerate(names):
            add(resource_id(category, name), category, name, f"registry:{category}:{index}")
    for record in trees["mcmeta-data-tree"]["tree"]:
        if record["type"] == "blob":
            identifier, category, name = data_identity(record["path"])
            add(identifier, category, name, f"data:{record['path']}")
    records = sorted(entries.values(), key=lambda entry: entry["id"])
    for record in records:
        record["sources"].sort()
    only_data = [record["id"] for record in records if record["category"] != "source_metadata" and not any(s.startswith("registry:") for s in record["sources"])]
    categories = Counter(record["category"] for record in records)
    summary = {
        "schema": "e01-source-catalog-summary/v1",
        "candidate": "Minecraft Java Edition 1.21.1",
        "status": "source_catalog_only_not_frozen",
        "registry_categories": len(registries),
        "registry_entries": sum(map(len, registries.values())),
        "data_tree_files": sum(x["type"] == "blob" for x in trees["mcmeta-data-tree"]["tree"]),
        "catalog_records": len(records),
        "source_mappings": sum(len(x["sources"]) for x in records),
        "catalog_by_category": dict(sorted(categories.items())),
        "empty_registry_categories": sorted(k for k, v in registries.items() if not v),
        "data_entries_missing_from_registry": only_data,
        "content_completeness": "unverified",
        "scoring_leaves": None,
        "denominator": None,
        "coverage": None,
    }
    return records, summary


def write_catalog(result):
    records, summary = result
    # One record per line keeps source mappings inspectable without a large expanded file.
    body = "".join(json.dumps(row, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n" for row in records)
    (ROOT / "catalog.jsonl").write_text(body, encoding="utf-8")
    (ROOT / "summary.json").write_bytes(encode(summary))


def check(source_query=None, content_query=None):
    sources = json.loads((ROOT / "sources.json").read_text())
    records, summary = derive(sources)
    stored = [json.loads(line) for line in (ROOT / "catalog.jsonl").read_text().splitlines()]
    if stored != records or json.loads((ROOT / "summary.json").read_text()) != summary:
        raise ValueError("catalog or summary differs from complete deterministic source projection")
    if source_query:
        found = [r for r in records if source_query in r["sources"]]
    elif content_query:
        found = [r for r in records if content_query == r["id"]]
    else:
        found = {k: v for k, v in summary.items() if k not in ("catalog_by_category", "data_entries_missing_from_registry")}
    if (source_query or content_query) and len(found) != 1:
        raise ValueError("query must identify exactly one source/catalog record")
    print(json.dumps(found, ensure_ascii=False, sort_keys=True, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--collect", action="store_true", help="fetch bounded text sources once; refuses to replace existing evidence")
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--source", help="look up a source reference, e.g. data:data/minecraft/recipe/acacia_boat.json")
    group.add_argument("--content", help="look up a stable catalog ID; returns all source references")
    args = parser.parse_args()
    if args.collect:
        collect()
    check(args.source, args.content)
