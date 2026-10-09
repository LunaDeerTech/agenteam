#!/usr/bin/env python3
"""Two closed independent tops using the accepted resource supervisor.

Preparation only prints a separate compilation command. It never compiles or
starts fixtures implicitly. Each --case needs the assigned exclusive resource
window; no batch/retry or extra browser/top budget is provided.
"""
import argparse
import ctypes
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import sys
import uuid

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
SOURCE = Path(__file__).resolve().parent
OUTPUT = ROOT / "output/ai/model-ui-independent"
ACCEPTED = ROOT / "output/ai/model-ui-recovery"
DELIVERY = ROOT
GO = "/workspace/toolchains/go1.27.1/bin/go"
BINARY = OUTPUT / "account-independent.test"
TOPS = {
    "a": "TestIndependentProjectModelsWebUnknownOriginalCombination",
    "b": "TestIndependentProjectModelsWebCurrentAuthorityArchivedRecovery",
}
spec = importlib.util.spec_from_file_location("independent_owned_supervisor", ROOT / ".agent-state/model-ui-regression/run-owned-regression.py")
supervisor = importlib.util.module_from_spec(spec)
spec.loader.exec_module(supervisor)
require, identity = supervisor.require, supervisor.identity
from owned_resources import merge, publish, read_descriptor


def selector(case):
    require(case in TOPS, "INDEPENDENT_CASE_REJECTED")
    return "^" + TOPS[case] + "$"


def shim(args):
    case = os.environ["MODELS_INDEPENDENT_CASE"]
    exact = selector(case)
    require(os.environ["REGRESSION_EXACT_SELECTOR"] == exact, "INDEPENDENT_SELECTOR_REJECTED")
    nonce = os.environ["MODELS_INDEPENDENT_NONCE"]
    require(len(nonce) == 32 and all(c in "0123456789abcdef" for c in nonce), "INDEPENDENT_NONCE_REJECTED")
    private = Path("/tmp") / ("mi-" + nonce[:12])
    evidence = OUTPUT / ("owned-" + case + "-" + nonce)
    require(os.environ["MODELS_INDEPENDENT_PRIVATE"] == str(private) and os.environ["MODELS_INDEPENDENT_EVIDENCE"] == str(evidence), "INDEPENDENT_PATH_REJECTED")
    for path in (private, evidence):
        identity(path)
        require(stat.S_IMODE(path.stat().st_mode) == 0o700, "INDEPENDENT_PRIVATE_MODE_REJECTED")
    if args == ["env", "GOVERSION"]:
        os.execv(GO, [GO, *args])
    if len(args) == 4 and args[:2] == ["build", "-o"] and args[3] in supervisor.adapter.BUILD:
        target = Path(args[2])
        require(target.is_absolute() and target.parent.resolve() == target.parent and target.parent.is_relative_to(private), "INDEPENDENT_HELPER_DESTINATION_REJECTED")
        source = ACCEPTED / "helpers" / supervisor.adapter.BUILD[args[3]]
        require(stat.S_ISREG(source.lstat().st_mode), "INDEPENDENT_HELPER_SOURCE_REJECTED")
        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o500)
        with os.fdopen(fd, "wb") as output, source.open("rb") as original:
            shutil.copyfileobj(original, output)
        return 0
    resources = {}
    for variable, family in [("AGENTEAM_OBJECT_FIXTURE", "object"), ("AGENTEAM_OUTBOUND_FIXTURE", "outbound"), ("AGENTEAM_PG_FIXTURE", "postgres"), ("AGENTEAM_PG_UNSUPPORTED_FIXTURE", "postgres")]:
        merge(resources, read_descriptor(os.environ[variable], family, private))
        publish(evidence / "adapter-resources.json", resources)
    require(supervisor.adapter.resource_shape(resources), "INDEPENDENT_RESOURCE_SHAPE_REJECTED")
    require(args == ["test", "-tags=integration", "-race", "-count=1", "-timeout=6m", "-run=" + exact, *supervisor.adapter.PACKAGES], "INDEPENDENT_ARGV_REJECTED")
    os.chdir(ROOT / "tests/account")
    os.execv(BINARY, [str(BINARY), "-test.v", "-test.count=1", "-test.timeout=6m", "-test.run=" + exact])


def prepare():
    OUTPUT.mkdir(mode=0o700, parents=True, exist_ok=True)
    identity(OUTPUT)
    overlay = OUTPUT / "overlay.json"
    supervisor.write_json(overlay, {"Replace": {str(DELIVERY / "tests/account/model_independent_test.go"): str(SOURCE / "independent_test.go")}})
    print(json.dumps({
        "cwd": str(DELIVERY),
        "environment": {"GOTOOLCHAIN": "local", "GOCACHE": str(OUTPUT / "go-cache"), "GOMODCACHE": str(ACCEPTED / "go-mod"), "GOMAXPROCS": "2", "GOPROXY": "off", "GOSUMDB": "off"},
        "command": [GO, "test", "-p=2", "-tags=integration", "-race", "-c", "-overlay=" + str(overlay), "-o", str(BINARY), "./tests/account"],
        "discovery": [str(BINARY), "-test.list=^TestIndependentProjectModelsWeb(UnknownOriginalCombination|CurrentAuthorityArchivedRecovery)$"],
        "compiled": False,
    }, indent=2))


def frozen_inputs():
    for name in ("project_owner_models_web_fixture_test.go", "project_owner_models_web_test.go"):
        require((ROOT / "tests/account" / name).read_bytes() == (DELIVERY / "tests/account" / name).read_bytes(), "INDEPENDENT_FIXTURE_SOURCE_MISMATCH")
    paths = [SOURCE / name for name in ("independent_test.go", "independent.spec.ts", "independent.config.mjs", "run-independent.py")]
    paths += [ROOT / "tests/account" / name for name in ("project_owner_models_web_fixture_test.go", "project_owner_models_web_test.go")]
    paths += [ROOT / ".agent-state/model-ui-regression" / name for name in ("run-owned-regression.py", "fixture-go.py")]
    paths += [ROOT / ".agent-state/model-ui-recovery" / name for name in ("owned_resources.py", "native-client-probe.ts", "authority-and-identity.ts", "validate-same-body.py")]
    paths += [ROOT / "api/openapi" / name for name in ("common.json", "project-models.json", "project-model-credentials.json")]
    paths += [ROOT / "docs/development/work-items" / name for name in ("d27-project-owner-model-settings-ui.md", "d27-project-owner-model-settings-ui-endpoints.json")]
    paths += [BINARY, ACCEPTED / "client-probe/native-client-probe.js", ROOT / "tests/account-captcha-web/package-lock.json", ROOT / "output/ai/deps-minio/bin/minio"]
    paths += [ACCEPTED / "helpers" / name for name in supervisor.adapter.BUILD.values()]
    inputs = {}
    for path in paths:
        require(stat.S_ISREG(path.lstat().st_mode), "INDEPENDENT_INPUT_NOT_REGULAR")
        inputs[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
    assets = supervisor.tree(ACCEPTED / "dist")
    inputs.update({str(ACCEPTED / "dist" / name): digest for name, digest in assets.items()})
    for path in (ROOT / "tests/account-captcha-web/node_modules/@playwright/test/cli.js", Path("/usr/bin/chromium"), Path(GO)):
        require(path.is_file() and os.access(path, os.X_OK if path.name != "cli.js" else os.R_OK), "INDEPENDENT_EXECUTABLE_MISSING")
    require(json.loads((ROOT / "tests/account-captcha-web/node_modules/@playwright/test/package.json").read_bytes())["version"] == "1.56.1", "INDEPENDENT_PLAYWRIGHT_VERSION_MISMATCH")
    require(os.access(BINARY, os.X_OK), "INDEPENDENT_BINARY_NOT_EXECUTABLE")
    return inputs, assets


def run(case):
    OUTPUT.mkdir(mode=0o700, parents=True, exist_ok=True)
    identity(OUTPUT)
    lockroot = ROOT / "output/ai/model-ui-regression"
    lockroot.mkdir(mode=0o700, parents=True, exist_ok=True)
    identity(lockroot)
    lock = os.open(lockroot / "driver.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    info = os.fstat(lock)
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o600, "INDEPENDENT_LOCK_REJECTED")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    for marker in (lockroot / "active-run.json", OUTPUT / "active-run.json"):
        require(not marker.exists() and not marker.is_symlink(), "INDEPENDENT_PREVIOUS_RUN_NOT_RETIRED")
    free = shutil.disk_usage(ROOT).free
    available = next(int(line.split()[1])*1024 for line in Path("/proc/meminfo").read_text().splitlines() if line.startswith("MemAvailable:"))
    require(free >= 5*1024**3 and shutil.disk_usage('/tmp').free >= 5*1024**3 and available >= 5*1024**3, "INDEPENDENT_FRESH_FIVE_GIB_GATE_FAILED")
    inputs, assets = frozen_inputs()
    frozen_hash = hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest()
    nonce = uuid.uuid4().hex
    evidence = OUTPUT / ("owned-"+case+"-"+nonce)
    evidence.mkdir(mode=0o700)
    # Share the existing driver's retained-failure gate as well as its lock.
    # A failed independent retirement must also block a subsequent old top.
    active = lockroot / "active-run.json"
    supervisor.write_json(active, {"case": case, "evidence": str(evidence), "nonce": nonce})
    active_id = (active.stat().st_dev, active.stat().st_ino)
    private = Path('/tmp') / ('mi-'+nonce[:12])
    private_id = runtime_id = None
    launched, retired, code = False, False, 1
    facts = {"started": False, "retirement_complete": False}
    try:
        supervisor.write_json(evidence / "inputs.json", {"files": inputs, "input_hash": frozen_hash, "go_source_tree": str(DELIVERY), "browser_case_seconds": 45, "top_seconds": 120, "tcp_tail_seconds": 75})
        private.mkdir(mode=0o700); private_id = identity(private)
        (private / 'browser').mkdir(mode=0o700); runtime_id = identity(private / 'browser')
        libc = ctypes.CDLL(None, use_errno=True)
        require(libc.prctl(36, 1, 0, 0, 0) == 0, "INDEPENDENT_SUBREAPER_UNAVAILABLE")
        env = {k:v for k,v in os.environ.items() if not k.startswith(('AGENTEAM_', 'MODELS_', 'REGRESSION_')) and k not in ('DEBUG','PWDEBUG')}
        env.update({"AGENTEAM_GO": str(SOURCE / "run-independent.py"), "MODELS_INDEPENDENT_ROOT": str(ROOT), "MODELS_INDEPENDENT_CASE": case, "MODELS_INDEPENDENT_NONCE": nonce, "MODELS_INDEPENDENT_PRIVATE": str(private), "MODELS_INDEPENDENT_EVIDENCE": str(evidence), "REGRESSION_EXACT_SELECTOR": selector(case), "TMPDIR": str(private), "GOTOOLCHAIN": "local", "PYTHONDONTWRITEBYTECODE": "1", "AGENTEAM_MINIO_BINARY": str(ROOT / "output/ai/deps-minio/bin/minio"), "AGENTEAM_AUTH_WEB_RUNTIME": str(private / "browser"), "AGENTEAM_PROJECT_MODELS_WEB_DIST": str(ACCEPTED / "dist"), "AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE": str(evidence), "AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH": frozen_hash})
        launched = True
        code, facts = supervisor.run_owned(env, private, evidence)
        retired = facts['retirement_complete']
        unchanged = all(Path(path).is_file() and hashlib.sha256(Path(path).read_bytes()).hexdigest() == digest for path,digest in inputs.items()) and supervisor.tree(ACCEPTED / 'dist') == assets
        facts['inputs_unchanged'] = unchanged
        if not unchanged: code = 1
    except Exception:
        code = 1; facts['failure'] = 'INDEPENDENT_SUPERVISOR_FAILED'
    finally:
        if not launched and private_id is not None:
            require(identity(private) == private_id, 'INDEPENDENT_PRIVATE_IDENTITY_CHANGED')
            if runtime_id is not None:
                require(identity(private / 'browser') == runtime_id, 'INDEPENDENT_RUNTIME_IDENTITY_CHANGED')
                (private / 'browser').rmdir()
            private.rmdir()
        facts.update({'case':case,'selector':selector(case),'exit':code,'assets_exclusively_private':True})
        supervisor.write_json(evidence / 'terminal.json', facts)
        if (retired or not launched) and not private.exists():
            require((active.stat().st_dev,active.stat().st_ino) == active_id, 'INDEPENDENT_ACTIVE_MARKER_CHANGED')
            active.unlink()
        os.close(lock)
    print(json.dumps({'evidence':str(evidence),'exit':code}))
    return code


if __name__ == '__main__':
    os.umask(0o077)
    try:
        if sys.argv[1:2] and sys.argv[1] in ('env','build','test'):
            sys.exit(shim(sys.argv[1:]))
        parser = argparse.ArgumentParser()
        mode = parser.add_mutually_exclusive_group(required=True)
        mode.add_argument('--prepare', action='store_true')
        mode.add_argument('--case', choices=TOPS)
        args = parser.parse_args()
        sys.exit(prepare() if args.prepare else run(args.case))
    except Exception:
        print('INDEPENDENT_OWNED_DRIVER_REJECTED', file=sys.stderr)
        sys.exit(1)
