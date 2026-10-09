#!/usr/bin/env python3
"""Offline independent probes, actual Skill methods with disclosed doubles."""
from pathlib import Path
import json, os, subprocess, tempfile
root=Path(__file__).resolve().parents[2]
output=root/'output/ai/skills-p2-independent'
output.mkdir(parents=True,exist_ok=True)
tmp=output/'tmp';tmp.mkdir(exist_ok=True)
env=dict(os.environ,GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOPROXY='off',GOSUMDB='off',GOTELEMETRY='off',GOMAXPROCS='2',GOMODCACHE=os.environ.get('GOMODCACHE','/workspace/agenteam/output/ai/model-ui-recovery/go-mod'),GOCACHE=os.environ.get('GOCACHE','/workspace/agenteam-project-variables-ui/output/ai/project-variables-ui/implementation/gocache'),GOTMPDIR=str(tmp))
subprocess.run(['git','diff','--exit-code','eaad50fd','--','internal/central/skill'],cwd=root,check=True,stdout=subprocess.DEVNULL)
with tempfile.TemporaryDirectory(prefix='pure-',dir=output) as work:
 overlay=Path(work)/'overlay.json'
 overlay.write_text(json.dumps({'Replace':{str(root/'internal/central/skill/zz_independent_p2_test.go'):str(Path(__file__).with_name('pure_test.go'))}}))
 result=subprocess.run([os.environ.get('AGENTEAM_GO','/workspace/toolchains/go1.27.1/bin/go'),'test','-mod=readonly','-p=1','-race','-vet=off','-count=1','-timeout=30s','-overlay',str(overlay),'-run','^TestIndependentSkill(ConfirmationCurrentRecheck|PackageActualReturnBoundaries)$','./internal/central/skill'],cwd=root,env=env,timeout=45,check=False)
 raise SystemExit(result.returncode)
