# Task Timeline 独立纯审

目标树 `/workspace/agenteam-task-timeline`，四技术文件按 `6c256ff49c61886204d732d47b1d740404a73a61` 冻结；本审未参与实现、不改其源。SPEC为 `d11-task-timeline-reader.md` rev1（cf912e3e）。

有限源码接受，无已发现must-fix。独立 `reader_review_test.go` 调用原public ListTaskEvents/read与原postgres.Rows；`rows_bridge.go`仅暴露原Rows的test-only构造。Store/ProjectAuthority及raw pgx.Rows均受控，不能据此声称真实Owner授权、PG Tx或物理lease/锁复用。

实际 `23681/acd731` race exit0/1.032s，2top5sub：双向查询的原W/seek/limit+1与每页授权顺序；撤销拒绝后没有额外Work SQL；坏lookahead及EOF错误无partial；取消后真实Rows.Close等待受控raw.Close、release随后才发生，Reader还须等待受控WithinTx尾。自有goroutine在每个尾分支实际join。作者strict codec两top另由本实例执行 `52491/24163d` race exit0/1.227s，复用四源已有cursor scalar/owner/project/task/filter/order负控静态核查。

零PG/socket/browser/network，未验收真实授权变更/同微秒UUID排序/SQL谓词执行和锁竞争、物理取消恢复。已有TaskReader.read逐页Normalize一次User/Project/Schedule/Task SH、当前Owner Read后loadTask，源码边界合理；这些真实矩阵仍归后续PG。

重建纯overlay并执行（只写ignored output；不修改目标源码）：

```sh
python3 - <<'PY'
import json
from pathlib import Path
root=Path('/workspace/agenteam-task-timeline')
probe=Path('/workspace/agenteam-knowledge/.agent-state/task-timeline-review')
out=Path('/workspace/agenteam-knowledge/output/ai/knowledge/timeline-review')
out.mkdir(parents=True,exist_ok=True)
(out/'overlay.json').write_text(json.dumps({'Replace':{str(root/'internal/central/postgres/timeline_review_bridge.go'):str(probe/'rows_bridge.go'),str(root/'internal/central/work/timeline_independent_review_test.go'):str(probe/'reader_review_test.go')}}))
PY
cd /workspace/agenteam-task-timeline
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -overlay=/workspace/agenteam-knowledge/output/ai/knowledge/timeline-review/overlay.json -run '^TestTimelineIndependent' ./internal/central/work
env PATH=/workspace/toolchains/go1.27.1/bin:$PATH GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod GOCACHE=/workspace/agenteam-knowledge/output/ai/knowledge/go-cache /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -p=1 -race -count=1 -run '^TestTaskTimeline' ./internal/central/work/contract
```
