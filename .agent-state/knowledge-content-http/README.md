# 正文 HTTP 有限入口

本工具片段复用本树既有 supervisor、root adapter 与 native driver，仅新增下列两个闭集 literal。旧默认与所有旧 selector 可逆为 `e3145974` 原字节，不造新的资源监督者。当前仅源码和替身控制就绪，PG/native candidate、native driver 都尚未构建，未运行任何真实资源。

| 模式 | 唯一新增 selector | 必须实际出现的节点 |
| --- | --- | --- |
| root chain，Account/B02/D05 真实 PG | `^TestKnowledgeOwnerContentHTTP(CurrentBytes\|CurrentAuthority\|ReaderOwnership\|ReadTransactions)$` | 4 top、14 sub，RUN/PASS各恰一次 |
| 非 root，原生 HTTP | `^TestContentHTTPNative(Deadlines\|KeepAliveAndClose\|BackpressureAndDisconnect)$` | 3 top、6 sub，RUN/PASS各恰一次 |

表格内反斜杠仅 Markdown 转义；下面 shell 参数中的 selector 无反斜杠。前者仍由原 `test-objects.sh` 链拥有 7 个资源与 3 个 private 目录。后者只有自身 listener/connection/handler，测试端口使用明确的 Account/领域替身，不是生产 root、SQL 权限或 D05 证明。

原预算不变：PG Go 6m/root 540s+TERM 60s+KILL 3s，native Go 90s/driver 105s/supervisor 123s+3s；原后代、资源双次 absent/private/runtime、actual Wait/reap、host TCP 75s 双次空与输入尾不变。新严格节点/Wait检查失败也继续原完整尾；native 要求原 manifest child 与 Start/actual Wait0 同 PID、原 runtime/private 成功且 tmp 实际不存在。PG 原 test Wait 必须恰一条 code0。

新分支输入包含真实 `tests/knowledge/*.go` fixture/业务来源、contenthttp、现行全部非测试生产 Go/模块/SQL/embed、原完整帧 commitproxy、新 Schema/common 与 schema-controls；根模式继续包含原 Go/MinIO/scripts 输入。结束时重新枚举路径集合再逐一 hash，新增/删除文件、读取错误或原值变化不能通过。此输入比较只是运行一致性，不替代候选编译、源码方法或业务验收。

无需 Go/资源的控制：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -B .agent-state/knowledge-content-http/selector-controls.py
```

首实际 `b17961` 通过148项：精确节点删/重/缺/错/skip/非法UTF8、固定配置正负例、实际 supervisor main 的显式 Child/资源/TCP/proc 替身，覆盖错误 Wait/退出码、资源/runtime残留、输入新增/删除与完整尾；三工具逐字逆差异。不调用实际 child/proc/PG/socket，也未证明真正节点可发现。

候选准备后，真实命令分别如下。下列路径是预定的新候选/输出，当前不能运行；每次必须 root 单独授予真实窗口，首次同进程打印 UTC/statvfs≥5GiB、核新输出及完整固定环境，禁止自动重试或借 pure/compiled 推 PASS。

```sh
python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --root-chain \
  --driver "$PWD/.agent-state/work-owner-http/root_chain_driver.py" \
  --binary "$PWD/output/ai/knowledge-content-http/content-pg-race-01.test" \
  --run '^TestKnowledgeOwnerContentHTTP(CurrentBytes|CurrentAuthority|ReaderOwnership|ReadTransactions)$' \
  --output "$PWD/output/ai/knowledge-content-http/pg-01"

python3 .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver "$PWD/output/ai/knowledge-content-http/content-native-driver-01" \
  --binary "$PWD/output/ai/knowledge-content-http/content-native-race-01.test" \
  --run '^TestContentHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$' \
  --output "$PWD/output/ai/knowledge-content-http/native-01"
```

PG 另须显式 `AGENTEAM_KNOWLEDGE_CONTENT_SCHEMA_PYTHON` 指向已存在本地 jsonschema/referencing 解释器；未配置即业务失败，不能把 Schema skip 当通过。root adapter 校验的固定 MinIO 尚待 root 授予普通复制；不在本任务自行下载或安装。Go 缓存当前已交还，后继编译同样另排。
