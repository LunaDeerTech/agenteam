# Execution Mount Capture 历史材料恢复

原工作树 `/workspace/agenteam-execution-mount-capture`，最终 tip `f55f53fd53b010a0302ea469dc6b87e2f1233bb5`；正式交付基准为 main `562d3538`。20 个产品及专属技术文件逐字一致；主线两 harness 还保有已交 AgentRunner UI profile，历史恢复只在隔离目录进行。

## 原件位置

2026-10-11 将原树 `output/ai/execution-mount-capture` 整目录以同文件系统、禁止覆盖的 rename 移至 [保全目录](../../output/ai/retired-20261011/execution-mount-capture/)，迁移时保持 inode、mode、uid、gid、mtime。现有 main `output/ai/execution-mount-capture/merge-method01.json` 未覆盖、未合并。迁移事实见 [preservation-record.json](../../output/ai/retired-20261011/execution-mount-capture/preservation-record.json)。

`/tmp/emc01` 原目录保留，小副本在保全目录 `original-tmp/emc01`；逐文件字节、模式、mtime 相等。整个 checks、原 unit01 FAIL、unit02、App compile、PG compile/list、native01 的输入/二进制/实际 Wait/资源尾及原 `run-check.py`、`run-check02.py`、`run-compile.py`、`prepare-native.py` 都随目录保存，不重造记录。

native01 原 1 top/2 sub 及全资源尾 whole PASS 保持。Unknown 场景是业务已实际 committed 后的受控返回丢失，不是物理 PG Unknown；本片不声称默认 App、物理 workspace 或 executor 运行已验。

## 有界源码恢复

从 main `562d3538` 建立**隔离恢复目录**，选择下表；禁止将历史补丁应用 main。

| 补丁 | 应用基础 | 用途 |
| --- | --- | --- |
| [retired-inputs.patch](retired-inputs.patch) | main `562d3538` | 还原 `f55f53fd` 的两个原 harness，并删除原 PG 同包闭包中尚不存在的 AgentRunner UI Go fixture，恢复原 72 个同包 Go 输入范围。 |
| [retired-inputs-806e0a68.patch](retired-inputs-806e0a68.patch) | 默认补丁之后 | 原 unit01 FAIL 的三个 preparation/runner 测试差额。 |

在隔离目录实际使用 `patch --batch --fuzz=0 --forward -p1 -i <补丁绝对路径>`，两补丁均 Wait 0、无 fuzz/offset，恢复文件逐字等历史 Git source；有原 manifest 的变更文件 SHA 同时匹配。结果见 [restore-checks](../../output/ai/retired-20261011/execution-mount-capture/restore-checks/)。原 native01 的 1744 个 tracked source 与 unit01 的 1679 个 tracked source 已用 Git blob 身份核对恢复，无全树重新 hash，见 [source-closure-check.json](../../output/ai/retired-20261011/source-closure-check.json)。

现有 `profile-controls.py` 未改。该控制记录中的历史 BASE `806e0a68` 两 harness 字节等于可达 main `33591391` 对应文件，别名已记入上述 source-closure 原件，无需额外重复保存整份 harness。原控制/配方内的旧 ref 和绝对路径保持历史值；恢复者须在隔离环境明确映射，不声称搬迁后原命令可直接运行。

## 退役边界

Runtime 与当前作者确认只从 main/新任务树工作，无旧树 source/output/binary/deps 活动依赖，也无旧 writer 或后台。热 Go cache、只读 module/MinIO 位于受保护的其他树，本次未动。这里只保全现存原件及必要历史差额，未重新执行 Go/测试/native/网络，未写 Git 或删除工作树/分支；最终清理由主线程另行处理。
