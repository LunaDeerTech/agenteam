#!/usr/bin/env python3
"""Closed old-regression adapter; no compilation during an owned browser top."""
import os
from pathlib import Path
import re
import shutil
import stat
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
ACCEPTED = ROOT / "output/ai/model-ui-recovery"
OUTPUT = ROOT / "output/ai/model-ui-regression"
sys.path.insert(0, str(ROOT / ".agent-state/model-ui-recovery"))
from owned_resources import merge, publish, read_descriptor

GO = "/workspace/toolchains/go1.27.1/bin/go"
# group: (exact Go top, old fixture family, browser source stem, asset reader)
TOPS = {
    "auth-lifecycle": ("TestAccountAuthenticationWebSessionLifecycle", "authentication", "authentication", "global"),
    "auth-revocation": ("TestAccountAuthenticationWebRevocationAndExpiry", "authentication", "authentication", "global"),
    "owner-edit": ("TestAccountProjectOwnerWebEditAndRename", "project_owner", "project-owner", "owner"),
    "owner-recovery": ("TestAccountProjectOwnerWebOriginalRecovery", "project_owner", "project-owner", "owner"),
    "owner-identity": ("TestAccountProjectOwnerWebIdentityAndOwnership", "project_owner", "project-owner", "owner"),
    "audit-authority": ("TestAccountProjectOwnerAuditWebAuthorityAndRecovery", "project_owner_audit", "project-owner-audit", "audit"),
    "audit-navigation": ("TestAccountProjectOwnerAuditWebNavigationAndLayouts", "project_owner_audit", "project-owner-audit", "audit"),
    "provider-recovery": ("TestAccountSystemProvidersWebOutcomeRecovery", "system_providers", "system-providers", "global"),
    "model-recovery": ("TestAccountSystemModelsWebOutcomeRecovery", "system_models", "system-models", "global"),
    "selection-recovery": ("TestAccountSystemModelSelectionWebOutcomeRecovery", "system_model_selection", "system-model-selection", "global"),
    "summary-recovery": ("TestAccountSystemMeetingSummaryWebRecovery", "system_meeting_summary", "system-meeting-summary", "summary"),
    "summary-authority-navigation": ("TestAccountSystemMeetingSummaryWebAuthorityNavigation", "system_meeting_summary", "system-meeting-summary", "summary"),
    "personal-theme": ("TestAccountPersonalSettingsWebThemeAndNavigation", "personal_settings", "personal-settings", "global"),
    "system-audit-authority": ("TestAccountSystemAuditWebAuthorityAndOwnership", "system_audit", "system-audit", "global"),
}
BUILD = {
    "./tests/testsupport/objectstore/cmd/fixture": "object-fixture",
    "./tests/testsupport/outbound/cmd/fixture": "outbound-fixture",
    "./tests/testsupport/outbound/cmd/server": "outbound-server",
    "./tests/testsupport/postgres/cmd/fixture": "postgres-fixture",
}
PACKAGES = ["./internal/central/postgres/...", "./tests/database/...", "./internal/central/app/...", "./tests/process/...", "./tests/security/...", "./tests/outbox/...", "./internal/central/outbox/...", "./tests/account/...", "./tests/accountmail/...", "./internal/central/accountmail/...", "./internal/central/recoverylog/...", "./tests/project/...", "./internal/central/model/...", "./tests/model/...", "./tests/objects/...", "./internal/central/object/..."]


def require(condition, message):
    if not condition:
        raise ValueError(message)


def selection(group):
    require(group in TOPS, "REGRESSION_UNKNOWN_GROUP")
    return "^" + TOPS[group][0] + "$"


def resource_shape(resources):
    rows = list(resources.values())
    expected = {"agenteam.d05.objectfixture": (1, 1), "agenteam.d04.networkfixture": (1, 1), "agenteam.d03.fixture": (2, 1)}
    return len(rows) == 7 and all(
        (sum(row["label"] == label and row["kind"] == "container" for row in rows),
         sum(row["label"] == label and row["kind"] == "network" for row in rows)) == counts
        and len({row["nonce"] for row in rows if row["label"] == label}) == 1
        for label, counts in expected.items()
    )


def context():
    group = os.environ["REGRESSION_GROUP"]
    selector = selection(group)
    require(os.environ["REGRESSION_EXACT_SELECTOR"] == selector, "REGRESSION_SELECTOR_MISMATCH")
    nonce = os.environ["REGRESSION_NONCE"]
    require(re.fullmatch(r"[0-9a-f]{32}", nonce) is not None, "REGRESSION_NONCE_INVALID")
    private = Path("/tmp") / ("regress-" + nonce[:12])
    evidence = OUTPUT / ("owned-" + group + "-" + nonce)
    require(os.environ["REGRESSION_PRIVATE_ROOT"] == str(private) and os.environ["REGRESSION_EVIDENCE"] == str(evidence), "REGRESSION_PATH_MISMATCH")
    for path in (private, evidence):
        info = path.lstat()
        require(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700 and info.st_uid == os.getuid() and path.resolve() == path, "REGRESSION_DIRECTORY_INVALID")
    return selector, private, evidence


def main():
    args = sys.argv[1:]
    selector, private, evidence = context()
    if args == ["env", "GOVERSION"]:
        os.execv(GO, [GO, *args])
    if len(args) == 4 and args[:2] == ["build", "-o"] and args[3] in BUILD:
        target = Path(args[2])
        require(target.is_absolute() and target.parent.resolve().is_relative_to(private) and target.parent.resolve() == target.parent, "REGRESSION_HELPER_TARGET_INVALID")
        source = ACCEPTED / "helpers" / BUILD[args[3]]
        require(stat.S_ISREG(source.lstat().st_mode), "REGRESSION_HELPER_SOURCE_INVALID")
        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o500)
        with os.fdopen(fd, "wb") as output, source.open("rb") as original:
            shutil.copyfileobj(original, output)
        return 0
    resources = {}
    for variable, family in [("AGENTEAM_OBJECT_FIXTURE", "object"), ("AGENTEAM_OUTBOUND_FIXTURE", "outbound"), ("AGENTEAM_PG_FIXTURE", "postgres"), ("AGENTEAM_PG_UNSUPPORTED_FIXTURE", "postgres")]:
        merge(resources, read_descriptor(os.environ[variable], family, private))
        publish(evidence / "adapter-resources.json", resources)
    require(resource_shape(resources), "REGRESSION_RESOURCE_SHAPE_INVALID")
    require(args == ["test", "-tags=integration", "-race", "-count=1", "-timeout=6m", "-run=" + selector, *PACKAGES], "REGRESSION_FIXTURE_ARGV_INVALID")
    print("REGRESSION exact precompiled account top; four containers and three networks registered", flush=True)
    binary = ACCEPTED / "account-delivery.test"
    os.chdir(ROOT / "tests/account")
    os.execv(binary, [str(binary), "-test.v", "-test.count=1", "-test.timeout=6m", "-test.run=" + selector])


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:
        print("REGRESSION_OWNED_FIXTURE_ADAPTER_REJECTED", file=sys.stderr)
        sys.exit(1)
