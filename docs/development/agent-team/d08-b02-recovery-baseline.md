# D08 B02 恢复后的真实环境基线

2026-10-05，独立 `verification_worker`。**固定提交 `98262b49aa87c52cfb0c09586dd1b76f9aa1fd52` 的原 B02 组通过：完整 fixture 命令 exit 0，`tests/project` 55.433s。** 本次只证明恢复后的 PG/MinIO 环境及已提交 B02 基线可运行，不是正在实施的 B03-R2 产品验收。

输入通过 `git archive` 导出到独占 `/tmp/agenteam-d08-b02-baseline-2h_8imsq/snapshot`，未创建 worktree，未复制缓存或 node_modules，未改快照源码；主树活动实现没有进入本次测试。archive SHA-256 为 `f6d6d7713311953f57a7d808460e2d94be23c4eff1b9aaeef14a7d9b2d0fed3a`，见 [input.json](evidence/d08-b02-baseline/input.json)。[suite-inputs.json](evidence/d08-b02-baseline/suite-inputs.json) 保存测试文件、依赖锁及 driver 指纹和完整选中名称；结束后这些文件指纹不变。

实际于 11:01:19–11:03:33 UTC 执行一次：

```sh
sh scripts/test-objects.sh -run '^TestProjectB02'
```

执行目录为上述快照。原 driver 的 `-tags=integration -race -count=1 -timeout=6m`、测试集合、断言及 fixture 逻辑全部保留；21 个顶层名称包含一个 child helper 和 20 个业务测试。没有失败或重跑。其他包显示 `[no tests to run]`，不计这些模块通过。

实际环境及精确 argv 见 [command.json](evidence/d08-b02-baseline/command.json)：

- Go `/workspace/toolchains/go1.27.1/bin/go`，实际 `go1.27.1 linux/amd64`；`GOTOOLCHAIN=local`、`GOENV=off`、`GOWORK=off`、`GOPROXY=off`、`GOSUMDB=off`。
- 使用已有锁定依赖缓存 `/workspace/agenteam-dependency-cache/modcache`；GOCACHE 和所有临时目录新建且独占，没有复用其他作者活动编译缓存。
- `AGENTEAM_MINIO_BINARY=/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio`；原 fixture 校验 release `RELEASE.2025-10-15T17-29-55Z`、SHA-256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 后启动真实服务。
- Docker 28.4.0；空的任务专属 `DOCKER_CONFIG`，显式 `DOCKER_HOST=unix:///var/run/docker.sock`，移除继承的 context/TLS/certificate/API 覆盖变量。仅连接原 driver 创建的 nonce-owned fixture。
- 原 driver 验证实际 PostgreSQL 17.8、兼容性负例环境 PostgreSQL 16.12、pgvector 0.8.1，以及独立 MinIO TLS 和出站 socket。原 B02 包覆盖创建、Owner/Session、路径/名称、幂等、原子 Audit/Event/receipt/Touch、事务 Unknown、创建 claim 恢复及其原迁移检查。

[fixture.log](evidence/d08-b02-baseline/fixture.log) 是完整原始合并输出，SHA-256 `d70cc510314d8bc611cbaf0921da83b780edf0af512e4ec0d624f511f5c56208`；没有保留凭据或私有 fixture descriptor。复跑时先以同一提交 `git archive` 导出新目录，再为 `command.json` 中的四个任务目录配置新路径，保持工具链、锁定依赖、已校验 MinIO 与原命令不变。快照、构建缓存和二进制不纳入仓库。

Docker 窗口在归档报告前已交回。原 driver 报告三个 fixture nonce 清理完成，随后按观测到的每个 exact ID 独立检查：**4 个容器和 3 个网络全部不存在，任务 runtime 目录为空，任务工具/测试进程为空**，见 [cleanup.json](evidence/d08-b02-baseline/cleanup.json)。两个开始前已有的 dev-infra 容器保持原 ID 和 running 状态，本任务未连接或操作它们。

本次仅新增本文与五份精简证据，未修改产品、测试、脚本、旧卡、P1 报告或 P1 证据，未执行 Git 写操作。B03-R2 的新接受机制、停止/清理 runtime、D05 真停止、D08 HTTP/app 及 D10 初始化均不在本次通过范围。检查与任务资源已经停止，记录完成后冻结交付。
