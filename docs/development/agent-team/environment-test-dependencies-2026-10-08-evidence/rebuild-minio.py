import json,os,pathlib,sys
r=pathlib.Path(__file__).resolve().parent
oldroot='/workspace/scratch/fixture-recovery'
m=json.loads(pathlib.Path('/workspace/agenteam/docs/development/agent-team/environment-test-dependencies-2026-10-07-evidence/evidence/minio-build-04.meta.json').read_text())
argv=[s.replace(oldroot,str(r)) for s in m['argv']]
env={k:(os.environ[k] if v=='<inherited-present>' else v.replace(oldroot,str(r))) for k,v in m['env'].items() if v!='<inherited-present>' or k in os.environ}
os.chdir(m['cwd'].replace(oldroot,str(r)))
os.execve(sys.executable,[sys.executable,str(r/'run.py'),'minio-build-01',*argv],env)
