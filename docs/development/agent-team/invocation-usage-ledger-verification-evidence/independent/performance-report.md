# Independent ledger performance evidence

The fixed 1201-row Aggregate completed with all counts and sums in **1.378896329s** under the original **2s** service context and `-race`. No rows, validation, or budget were removed. This is one controlled fixture measurement, not a production throughput or arbitrary dataset guarantee.

Input was author `input07`, manifest `ec8a86c784d7ff64110ac774cb480de4c8a0492d72fdc0f4b974308f74729ff4`, plus private probe `usage_performance_independent_test.go` SHA `8ba8ea0b91c799a604dfcfb80325361ca801a524d131b90ca8456b814592a29c`. The probe uses one genuine wire row and 1200 explicitly synthetic statistical rows, runs ordinary `ANALYZE`, and forwards the actual SQL and arguments through the real Store. Synthetic rows do not establish 1200 Provider sends.

The original driver invocation and environment are in `perf-lookup01/command.json`; raw evidence is `perf-lookup01/raw.log`. That invocation has **exit 1** because the separately selected original Lookup publication-cancellation probe failed. The performance top itself passed. The old Lookup failure remains evidence for U-R02, not a performance failure.

| Measured boundary | Time |
| --- | ---: |
| Public Aggregate call, complete 1201 count and input sum | 1.378896329s |
| Group SQL plus group scan | 4.041593ms |
| Private 40-column transfer/scan/JSON/digest validation | 1.370235103s |
| Callback complete to actual commit return | 643.97µs |
| Separate group SQL EXPLAIN ANALYZE execution / planning | 2.545ms / 0.614ms |
| Separate private rows SQL EXPLAIN ANALYZE execution / planning | 1.575ms / 0.129ms |

The private scan matched all 1201 rows, so its Seq Scan is consistent with the selected complete population. It estimated 1201 rows, had 301 shared hits, zero disk reads and no temporary I/O. The card requires declared useful indexes and actual data/EXPLAIN, not one named index for every complete statistical scan. Author `real-02.log` separately verifies paged ordering uses `model_invocation_project_page` after ordinary `ANALYZE` and measures the complete Aggregate at **1.361191577s** on input11. Input13 changes only the Session test helper and Reader test.

CPU profile `perf-lookup01/aggregate.cpu` resolves against the exact matching test ELF build ID `6f7b95e4014f5bdfe0bf887988a586c97d255a68`. Commands, binary/profile hashes and exit codes are in `profile-analysis.json`; the final two pprof stderr files are empty. The initial missing-temp-binary warning is retained in `profile-binary-notes.txt`. Flat samples are dominated by race instrumentation; business cumulative samples include `scanInvocation` (0.48s), JSON marshal (0.32s), header/final digest work (0.16s each), and strict JSON decoding (0.09s). Cumulative entries overlap and must not be summed as independent phases. Evidence identifies in-process validation and instrumentation as the large cost, rather than slow SQL.

No production optimization is justified by a demonstrated budget failure in these fixed measurements. JSON, digest and private/public consistency checks remain mandatory. Larger populations can hit the fixed deadline and must return error with zero DTO; success here does not promise unlimited full-population success.

Correction preserved: author real01's **1.92s** was the failed subtest duration, including EXPLAIN, not an exact public Aggregate measurement. The original failure was an index-name assertion before data statistics were updated. Input08 added normal ANALYZE and precise API timing; it did not force planner switches, reduce data or increase the deadline.

Both cleanup passes in `perf-lookup01/cleanup.json` show seven exact Docker IDs absent, original 2-container/4-network identity/name/labels unchanged, no owned processes, and an empty private TMPDIR. The coordinator rechecked absence before returning the window at 01:18:08 UTC. No existing infrastructure was connected.
