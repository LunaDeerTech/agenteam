#!/usr/bin/env python3
"""Project three pinned data-rule sources; this is not gameplay acceptance."""

import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parent
COMMIT = "7cffbcdad5fa36f22acef6bd4b869ecad4ca3fd9"
KINDS = ("advancement", "recipe", "enchantment")


def encode(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def pointer(*parts):
    return "/" + "/".join(str(p).replace("~", "~0").replace("/", "~1") for p in parts)


def source_url(kind):
    return f"https://raw.githubusercontent.com/misode/mcmeta/{COMMIT}/data/{kind}/data.min.json"


def collect():
    path = ROOT / "behavior-sources.json"
    if path.exists():
        raise ValueError("behavior-sources.json exists; preserve this revision")

    def fetch(kind):
        request = urllib.request.Request(source_url(kind), headers={"User-Agent": "agenteam-e01-behavior-catalog/1"})
        with urllib.request.urlopen(request, timeout=30) as response:
            body = response.read(1_000_001)
            if len(body) > 1_000_000:
                raise ValueError(f"source too large: {kind}")
        return kind, {"url": source_url(kind), "bytes": len(body), "sha256": hashlib.sha256(body).hexdigest(), "body": body.decode()}

    with ThreadPoolExecutor(max_workers=3) as pool:
        sources = dict(pool.map(fetch, KINDS))
    validate_sources(sources)
    path.write_bytes(encode(sources))


def validate_sources(sources):
    if set(sources) != set(KINDS):
        raise ValueError("missing or unknown behavior source")
    old = json.loads((ROOT / "sources.json").read_text())
    tree = {row["path"]: row for row in json.loads(old["mcmeta-summary-tree"]["body"])["tree"]}
    for kind, source in sources.items():
        body = source["body"].encode()
        blob = tree[f"data/{kind}/data.min.json"]
        if source["url"] != source_url(kind) or source["bytes"] != len(body) or source["sha256"] != hashlib.sha256(body).hexdigest():
            raise ValueError(f"changed source: {kind}")
        if blob["size"] != len(body) or blob["sha"] != hashlib.sha1(b"blob " + str(len(body)).encode() + b"\0" + body).hexdigest():
            raise ValueError(f"source does not match already pinned summary blob: {kind}")
    return {key: json.loads(value["body"]) for key, value in sources.items()}


def project(sources):
    definitions = validate_sources(sources)
    catalog_bytes = (ROOT / "catalog.jsonl").read_bytes()
    catalog = {row["id"]: row for row in map(json.loads, catalog_bytes.decode().splitlines())}
    rules = []
    tag_references = 0
    for kind in KINDS:
        expected = {row["name"] for row in catalog.values() if row["category"] == kind}
        if set(definitions[kind]) != expected:
            raise ValueError(f"rule definitions do not match the existing {kind} catalog")
        for name, definition in sorted(definitions[kind].items()):
            references = []

            def reference(parts, category, value, force_tag=False):
                if isinstance(value, list):
                    for index, child in enumerate(value):
                        reference([*parts, index], category, child, force_tag)
                    return
                if not isinstance(value, str):
                    raise ValueError(f"unsupported typed reference: {kind}/{name} {parts}")
                tag = value.startswith("#") or force_tag
                resource = value.removeprefix("#")
                if not resource.startswith("minecraft:"):
                    raise ValueError(f"unknown resource namespace: {resource}")
                target_category = f"tag/{category}" if tag else category
                target = f"mcj/1.21.1/{target_category}/{resource}"
                if target not in catalog:
                    raise ValueError(f"reference missing from existing catalog: {target}")
                references.append({"pointer": pointer(name, *parts), "value": value, "catalog_id": target, "tag_members": "not_expanded" if tag else None})

            def ingredient(parts, value):
                if isinstance(value, list):
                    for index, child in enumerate(value):
                        ingredient([*parts, index], child)
                elif isinstance(value, dict) and set(value) in ({"item"}, {"tag"}):
                    key = next(iter(value))
                    reference([*parts, key], "item", value[key], key == "tag")
                else:
                    raise ValueError(f"unsupported ingredient shape: {kind}/{name} {parts}")

            if kind == "recipe":
                reference(["type"], "recipe_serializer", definition["type"])
                for key in ("ingredient", "ingredients", "addition", "base", "template"):
                    if key in definition:
                        ingredient([key], definition[key])
                for symbol, value in definition.get("key", {}).items():
                    ingredient(["key", symbol], value)
                if "result" in definition:
                    reference(["result", "id"], "item", definition["result"]["id"])
                family = definition["type"]
                limitation = "Input/result/timing fields only where present; serializer implementation, default values, fuel, UI and tag membership are not defined here."
            elif kind == "advancement":
                if "parent" in definition:
                    reference(["parent"], "advancement", definition["parent"])
                if "display" in definition:
                    reference(["display", "icon", "id"], "item", definition["display"]["icon"]["id"])
                for criterion, value in definition["criteria"].items():
                    reference(["criteria", criterion, "trigger"], "trigger_type", value["trigger"])
                for index, value in enumerate(definition.get("rewards", {}).get("recipes", [])):
                    reference(["rewards", "recipes", index], "recipe", value)
                family = name.split("/", 1)[0]
                limitation = "Criteria/requirements/rewards are advancement rules, not complete definitions of the underlying gameplay; nested predicate references are retained in source, not semantically expanded."
            else:
                for key, category in (("supported_items", "item"), ("primary_items", "item"), ("exclusive_set", "enchantment")):
                    if key in definition:
                        reference([key], category, definition[key])
                for effect in definition.get("effects", {}):
                    # These are object KEYS, not string values at those pointers.
                    target = f"mcj/1.21.1/enchantment_effect_component_type/{effect}"
                    if target not in catalog:
                        raise ValueError(f"unknown enchantment effect component: {effect}")
                    references.append({"pointer": pointer(name, "effects", effect), "object_key": effect, "catalog_id": target, "tag_members": None})
                family = "data_driven_enchantment"
                limitation = "Declared applicability/cost/effect fields only; missing effects do not prove no effect. Source weight is game selection weight, never E01 scoring weight."
            tag_references += sum(row["tag_members"] is not None for row in references)
            rules.append({
                "id": f"mcj/1.21.1/data-rule/{kind}/minecraft:{name}",
                "kind": kind,
                "family": family,
                "source": kind,
                "source_pointer": pointer(name),
                "catalog_id": f"mcj/1.21.1/{kind}/minecraft:{name}",
                "field_pointers": {key: pointer(name, key) for key in sorted(definition)},
                "typed_references": references,
                "scope_limit": limitation,
            })
    candidate = json.loads((ROOT / "candidate.json").read_text())
    preparation = candidate["behavior_preparation"]
    systems = preparation["system_directory"]
    if len({row["id"] for row in systems}) != len(systems) or preparation["system_directory_complete"] or preparation["rule_count_is_content_denominator"]:
        raise ValueError("invalid or falsely complete system directory")
    rule_ids = {row["id"] for row in rules}
    for system in systems:
        if not system["missing_behavior_sources"] or any(item not in rule_ids for item in system["rule_examples"]) or any(item not in catalog for item in system["catalog_examples"]):
            raise ValueError(f"invalid system gap or reference: {system['id']}")
    if any(value is not None for key, value in candidate["scoring"].items() if key != "reason"):
        raise ValueError("this partial preparation must not assign scoring values")
    return {
        "schema": "e01-versioned-data-rules/v1",
        "reference_version": "Minecraft Java Edition 1.21.1",
        "status": "partial_data_rules_not_complete_gameplay",
        "catalog_sha256": hashlib.sha256(catalog_bytes).hexdigest(),
        "source_sha256": {key: sources[key]["sha256"] for key in sorted(sources)},
        "scoring_leaves": None,
        "weights": None,
        "denominator": None,
        "coverage": None,
        "reference_scope": {
            "recipe": "Serializer, explicit ingredient item/tag references, and explicit result ID.",
            "advancement": "Parent, display icon, criterion trigger, and recipe rewards; full predicates/requirements remain at exact source pointers.",
            "enchantment": "Supported/primary item sets, exclusive enchantment sets, and effect component keys; nested effect/predicate definitions remain at exact source pointers.",
            "tags": "Tag identity only; no tag member body was fetched or inferred.",
        },
        "counts": {
            "rules_by_kind": {kind: len(definitions[kind]) for kind in KINDS},
            "rules": len(rules),
            "typed_references": sum(len(row["typed_references"]) for row in rules),
            "tag_references_not_expanded": tag_references,
            "recipe_by_serializer": dict(sorted(Counter(row["type"] for row in definitions["recipe"].values()).items())),
            "advancement_by_path_group": dict(sorted(Counter(name.split("/", 1)[0] for name in definitions["advancement"]).items())),
            "advancements_with_display": sum("display" in row for row in definitions["advancement"].values()),
            "recipes_without_explicit_result": sum("result" not in row for row in definitions["recipe"].values()),
            "enchantments_without_explicit_effects": sum("effects" not in row for row in definitions["enchantment"].values()),
        },
        "rules": rules,
    }


def check(sources, write=False, rule_query=None, concept_query=None):
    result = project(sources)
    path = ROOT / "behavior-catalog.json"
    if write:
        if path.exists():
            raise ValueError("behavior-catalog.json exists; do not silently replace a candidate")
        path.write_bytes(encode(result))
    if json.loads(path.read_text()) != result:
        raise ValueError("behavior catalog differs from exact source-field projection")
    if rule_query:
        matches = [rule for rule in result["rules"] if rule["id"] == rule_query]
        if len(matches) != 1:
            raise ValueError("rule query must identify one rule")
        rule = matches[0]
        name = rule["catalog_id"].split("minecraft:", 1)[1]
        output = {"rule": rule, "definition": json.loads(sources[rule["source"]]["body"])[name]}
    elif concept_query:
        output = [rule["id"] for rule in result["rules"] if rule["catalog_id"] == concept_query or any(row["catalog_id"] == concept_query for row in rule["typed_references"])]
        if not output:
            raise ValueError("no rule mapping for this concept; this does not prove absence of gameplay")
    else:
        output = {"status": result["status"], "counts": result["counts"], "denominator": None, "coverage": None}
    print(json.dumps(output, ensure_ascii=False, sort_keys=True, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--collect", action="store_true")
    parser.add_argument("--write", action="store_true", help="write the initial rule projection; refuses to replace a candidate")
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--rule", help="show a rule and its exact stored definition")
    group.add_argument("--concept", help="reverse lookup a source-catalog concept")
    args = parser.parse_args()
    if args.collect:
        collect()
    sources = json.loads((ROOT / "behavior-sources.json").read_text())
    check(sources, args.write, args.rule, args.concept)
