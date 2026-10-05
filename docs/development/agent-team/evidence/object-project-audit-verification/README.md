# Object Project Audit 验收证据

最终结论见 [独立验收报告](../../object-project-audit-verification.md)。本目录只有原始小文件、精确 patch 和可重建探针；不含 cache、数据库、凭据、构建产物或完整源码快照。

- `final-input.json`：固定 `6658a6c` 加最终 13 源；`final-source.patch` 精确复原它们。`committed-input-check.json` 将同一 13 源绑定到交付提交 `a716ae2a16bc24c115d0207557cada96a30f1049`。
- `reviews/`：原 review01 阻断、review02 修复差量和首红/测试修正静审报告，内容保持历史原样。它们的临时路径与“尚未动态验证”是当时状态；后续闭合见最终报告。旧固定依赖从基线提交读取，review01 生产可由最终生产逆向应用 `get-complete-delta.patch` 重建。
- `author/`：作者交接、逐项覆盖、实际命令/结果、原始成功及失败日志、资源记录。`inputs/` 保存 new1/new2/final-real 的原运行清单和两个精确测试 delta。作者 `integration-new-2` 整组仍是 FAIL；`coverage-check.json` 独立核对仅复用了日志中实际通过且输入未变的各项。
- `independent/`：独立实际命令、编译/原始真实日志、完整资源事件与二次清零。`driver-argument-failure/` 为 CLI `-v` 拒绝；`build-disk-failure/` 轮同时有 `/tmp` 满的多包编译失败及 6 子例初始化 `OBJECT_PAYLOAD_MISSING`，未触达目标行为，初始化错误的唯一原因未证，不计行为通过。`independent-real-3.log` 是迁移本人构建/cache/runtime 后原 2 顶层/6 子例全部通过。`probe-r1.go.txt` 和带 `-r1` 的纯日志仅绑定前版探针；最终 probe 是根目录 `independent-probe.go.txt`，最终 compile 与真实 race 均消费它。
- `SHA256SUMS` 校验本目录全部其它文件。其本身和最终报告的 SHA 在交接时单独提供。

最初简报原字节保存在 `independent/preliminary-summary-original.md`；其中磁盘轮“未进入 probe”的误述已在 [勘误](independent/summary-erratum.md) 和最终报告纠正，不能脱离勘误引用原简报。

## 重建输入

只读 Git 历史、在新的自有目录重建；默认不执行 Go、Docker，也不改工作树。需 Python 3、Git、tar、patch，以及持有固定基线的仓库。

```sh
python3 reproduce.py --repo /absolute/agenteam --output /absolute/task-owned-new-directory
```

`--sources-only` 只重建并逐项校验 13 个被审路径及最终探针，适合快速核对归档。`--revision new2`、`new1`、`late-get-red2` 分别逆向应用测试 delta 和 GET 兼容修复，最后必须逐字节匹配各次真实运行清单；这些旧版本不注入最终独立探针。早期纯编译/构造错误只保留原日志，不声称每个更早临时版本都已完整重建。

## 重跑独立真实增量

取得唯一 fixture 窗口后，在上述完整重建的 `tree/` 运行以下原 driver。使用自有空 Docker 配置、显式本机 socket、离线锁定依赖和有足够容量的私有 cache/runtime；启动前记录现存容器/网络的 ID、name、labels，结束后按原 `independent/run_real.py` 逻辑记录创建 ID 并两次验证全部 absent、原基线不变、进程和 runtime 为空。归档 runner 保留原绝对路径以证明实际执行；复跑时只更换自有目录。

```sh
export AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
export AGENTEAM_MINIO_BINARY=/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-v
export GOMODCACHE=/workspace/agenteam-dependency-cache/modcache
export GOCACHE=/absolute/task-owned-new-directory/gocache
export TMPDIR=/absolute/task-owned-new-directory/runtime
export DOCKER_CONFIG=/absolute/task-owned-new-directory/docker-config
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH DOCKER_AUTH_CONFIG
cd /absolute/task-owned-new-directory/tree
sh scripts/test-objects.sh -run '^TestObjectAuditIndependent'
```

原 helper 内的 `-race -count=1 -timeout=6m` 不变；脚本不接收单独 CLI `-v`。被 selector 排除的包会显示 `no tests to run`，不算它们的业务回归。若缺上述确切工具链/MinIO/已锁 modcache，先按仓库恢复文档准备，不能替换为模拟依赖或其它版本。
