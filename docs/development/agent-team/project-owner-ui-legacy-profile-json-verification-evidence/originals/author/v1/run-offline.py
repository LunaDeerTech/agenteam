import ctypes, hashlib, json, os, pathlib, re, signal, subprocess, sys, threading, time
root=pathlib.Path('/workspace/agenteam'); store=pathlib.Path('/workspace/scratch/owner-ui-recovery')
label=sys.argv[1]; command=sys.argv[2:]; run=store/label; run.mkdir(exist_ok=False)
ctypes.CDLL(None,use_errno=True).prctl(36,1,0,0,0)
def snapshot():
    names=[
        'tests/account-captcha-web/e2e/personal-settings.spec.ts',
        'tests/account-captcha-web/personal-settings.config.js',
        'tests/account-captcha-web/package.json',
        'tests/account-captcha-web/package-lock.json',
        'web/package.json', 'web/package-lock.json',
        'web/.prettierrc.json', 'web/.prettierignore',
        'web/node_modules/typescript/bin/tsc',
        'web/node_modules/typescript/lib/_tsc.js',
        'web/node_modules/typescript/package.json',
        'web/node_modules/prettier/bin/prettier.cjs',
        'web/node_modules/prettier/index.mjs',
        'web/node_modules/prettier/plugins/typescript.mjs',
        'web/node_modules/prettier/plugins/estree.mjs',
        'web/node_modules/prettier/package.json',
        'tests/account-captcha-web/node_modules/@playwright/test/package.json',
        'tests/account-captcha-web/node_modules/@playwright/test/index.d.ts',
        'tests/account-captcha-web/node_modules/playwright/types/test.d.ts',
        'tests/account-captcha-web/node_modules/playwright-core/types/types.d.ts',
        'web/node_modules/@types/node/package.json',
    ]
    paths=[root/name for name in names]
    paths += [pathlib.Path(__file__), pathlib.Path(sys.executable).resolve(), pathlib.Path(command[0])]
    return {str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(set(paths))}

def processes():
    result={}
    for path in pathlib.Path('/proc').glob('[0-9]*/stat'):
        try:
            fields=path.read_text().rsplit(')',1)[1].split(); result[int(path.parent.name)]=(int(fields[1]),fields[19])
        except (OSError, ValueError, IndexError): pass
    return result
before=snapshot(); (run/'inputs-before.json').write_text(json.dumps(before,indent=2)+'\n')
(run/'command.json').write_text(json.dumps({'argv':command,'cwd':str(root),'budget_seconds':40,'cleanup_seconds':4},indent=2)+'\n')
env={k:v for k,v in os.environ.items() if not k.startswith('AGENTEAM_')}
owned={}; done=threading.Event()
with (run/'raw.log').open('wb') as out:
    start=time.monotonic(); process=subprocess.Popen(command,cwd=root,env=env,stdout=out,stderr=subprocess.STDOUT,start_new_session=True)
    def monitor():
        while not done.is_set():
            current=processes(); ids={process.pid}
            for _ in range(8): ids|={pid for pid,(ppid,_) in current.items() if ppid in ids}
            for pid in ids:
                if pid in current: owned[pid]=current[pid][1]
            done.wait(.03)
    watcher=threading.Thread(target=monitor); watcher.start(); timed_out=False
    try: code=process.wait(timeout=40)
    except subprocess.TimeoutExpired:
        timed_out=True; os.killpg(process.pid,signal.SIGTERM)
        try: code=process.wait(timeout=1)
        except subprocess.TimeoutExpired: os.killpg(process.pid,signal.SIGKILL); code=process.wait(timeout=1)
    done.set(); watcher.join(); adopted=[]
    while True:
        try:
            pid,status=os.waitpid(-1,os.WNOHANG)
            if pid==0: break
            adopted.append([pid,status])
        except ChildProcessError: break
    def live(): return {pid:stamp for pid,stamp in owned.items() if processes().get(pid,(None,None))[1]==stamp}
    first=live(); time.sleep(.06); second=live()
after=snapshot(); (run/'inputs-after.json').write_text(json.dumps(after,indent=2)+'\n')
result={'exit':code,'timeout':timed_out,'elapsed_seconds':time.monotonic()-start,'direct_actual_wait':process.pid,'adopted_actual_wait':adopted,'owned_pids':owned,'owned_scan_1':first,'owned_scan_2':second,'inputs_same':before==after}
(run/'result.json').write_text(json.dumps(result,indent=2)+'\n'); print(json.dumps(result)); print((run/'raw.log').read_text()[-14000:]); sys.exit(0 if code==0 and not first and not second and (before==after or '--write' in command) else 1)
