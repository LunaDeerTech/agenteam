# native01 preparation only

Fixed accepted B is e7304512e73fdddb25bdbf1c0882400ad5b5f791; accepted Summary is e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a. All 291 actual product graph paths match fixed B Git bytes. The accepted 379-package/2349-source race closure hashes all match; active C app/fixture paths are absent. Generated test main has no custom TestMain, and each of the three native tops is opt-in guarded. The accepted race binary is SHA256 fc86e607bc6b9f5abebdca787634b06fb79ad8a012451ccb11235dbe9c7acf50. No source changes or native execution have occurred in preparation.

Driver SHA256 f8edfd51dec7a9244bf4ba5e0283b5163350a117c4cdd3ff83ecf36276b91f3e. The no-socket `/bin/sleep 0.25` tracing probe exited 0; direct PID 60221/starttime303688 and adopted tracer PID60225/starttime303688 were both actually waited with exit0. Both remained in pgrp60221. Two subsequent scans show their exact processes absent and zero task ports. `strace -D` uses grandchild mode; it does not use -DD/-DDD's different pgrp/session modes.

After root confirms the unique loopback window, exact entry command:

```
python3 /workspace/scratch/usage-http-verification/native01/run.py native
```

The driver starts:

```
/usr/bin/strace -D -f -ttt -yy -e trace=%network,%process,close -o /workspace/scratch/usage-http-verification/native01/native/trace.raw /workspace/scratch/usage-http-author/B/usage-http.test -test.count=1 -test.parallel=1 -test.timeout=45s '-test.run=^TestProjectUsageHTTPNative(SlowBody|WriteAndClose|KeepAlive)$' -test.v
```

Only AGENTEAM_USAGE_HTTP_NATIVE=1 is enabled, with offline/read-only Go configuration and GOMAXPROCS=2. This reuses the accepted `-race -p=1 -c` single-package binary. The package timeout remains45s, and the independent execution watchdog also sends TERM at45s. A separate15s allowance is solely for actual wait/cleanup; no rerun occurs. After actual waits, read-only socket observation may take up to75s for kernel TIME_WAIT to disappear; it is recorded distinctly from live listeners/connections. Two exact process/port-zero scans and full pre/post input digests are required.

Sources assert actual TCP EOF, slow body natural2s/earlier120ms deadlines, actual native write deadline, close/write/flush fault aborts and body/callback join, and six sequential GET/HEAD requests on one keepalive connection after the first deadline expires. Service/auth seams remain controlled: this does not establish C/root binding, real Session/PG authority, object fixtures or complete card acceptance. Existing unrelated resources and historical zombies are outside ownership. Workspace free25.0GB and tmp9.0GB exceed2GiB.
