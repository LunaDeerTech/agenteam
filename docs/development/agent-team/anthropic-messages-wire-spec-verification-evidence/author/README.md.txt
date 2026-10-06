# Anthropic Messages rev1 规格研究证据

正式候选：`/workspace/agenteam/docs/development/work-items/recovery-d09-anthropic-messages-wire.md`。
冻结原件：`spec-freeze01/spec.md`，SHA-256 `bc3830cf43ca262e560ac7668f2cc1547c33aedb264fef432bd9d00c2335d2b7`。
此目录只记录作者静态来源核验、工程候选与文档自查，不是独立验收或产品运行。

- `source-manifest.json`：固定官方 SDK 提交 `18f25547f20cf5f01da69ac611e700e3bc9ebf21`，66 个限定原文件，共297810 bytes；`source/` 保存完整原字节与 LICENSE。
- `commit-object.txt`、`fetch-commands.json`、`fetch-{0,1}.{stdout,stderr}`：仅公开 Git 对象读取；不安装或执行 SDK，无账号请求。`objects.git/` 仅取证临时对象库，后续归档不复制。
- `field-manifest.json`：九组采用事实的确切文件/行/原文，与平台工程决定分列；C0 的空文本、有序 Parts 依据也标注。
- `baseline-inputs.json`：接受基线 `0d22c7f` 的25个相关路径指纹，独立状态提交 `71f276f` 的两文档指纹；不消费活动 tools 代码作为已验输入。
- `verify-inputs.py` / `source-check.stdout`：实际核全部官方 SHA-256、Git blob/commit SHA-1、只读本仓库固定 Git 输入与字段行。运行 `python3 /workspace/scratch/agenteam-anthropic-spec-76c077li/verify-inputs.py`，不联网、不执行 SDK/产品。
- `spec-checks.json`、`spec-diff-check.*`：14个本地链接、2个fragment、19候选路径、11节/7表、UTF-8/LF/末尾换行/尾空格与结构自查通过。新增文件另做逐行检查；no-index检查exit1且无diagnostics表示新文件差异，不冒称该命令exit0。
- `spec-freeze01/manifest.json`：候选及上述关键元数据SHA与停止写入声明；实现尚未授权。
- `status-freeze01/`：独立的两份低影响状态文档原件/检查/原协调记录，已由root提交推送 `71f276f374fb1846adcd94bbc2dec2285be63e59`。它们不混入Anthropic实施路径。

当前 tools 原独立动态任务因自动安全筛查“possible cybersecurity risk”被中断且实际 NOT RUN，禁止重试、改派或换方式重建，用户确认仍待主线程处理。这个新规格不承担该任务，其实施必须等待 tools 最终接受和共享文件交权。reasoning/structured、生产 Resolver/Runtime/HTTP/root及完整D09仍是后续责任。

截至冻结，未运行 Go、Docker、浏览器、Provider 请求或任何外部 SDK 代码；没有改产品源码、依赖或 Git 索引/提交/分支。无后台研究命令或自有运行资源。后续只需归档 source、上述小文本/JSON与冻结卡；不要复制objects.git或工作区树。
