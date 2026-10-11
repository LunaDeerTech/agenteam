# Agent Runner Mounts UI 历史材料恢复

原工作树 `/workspace/agenteam-agent-runner-mounts-ui`，最终 tip `5771937a883b68368d3ddc8913b231c304ca01ad`；UI 正式交付基准为 main `94a225a2`，26 个专属技术文件逐字一致。现有 launcher/profile controls 保持原内容。

## 原件位置与结论边界

2026-10-11 将原树 `output/ai/agent-runner-mounts-ui` 整目录以同文件系统、禁止覆盖的 rename 移至 [保全目录](../../output/ai/retired-20261011/agent-runner-mounts-ui/)。原 `web/dist` 同样移至该目录 `original-web-dist`；两次 rename 的 inode、mode、uid、gid、mtime 在迁移时保持。具体原→新路径见 [preservation-record.json](../../output/ai/retired-20261011/agent-runner-mounts-ui/preservation-record.json)。

- `/tmp/armw01`、`/tmp/armw02` 原目录保留，小副本在 `original-tmp/armw01`、`original-tmp/armw02`；各文件字节、模式、mtime 相等。
- 全部 checks、纯控原 FAIL、compile-web01/02、native01/02、原 Wait、二进制、输入 manifest、dist-01/02、截图与请求联证均保留。`methods/list01-source/` 中原转译 JS/map 与行号说明也完整保留。
- compile-web01 未用局部变量导致的原编译 FAIL、native01 初始表单焦点 FAIL 保持；native01 浏览器没有写业务。native02 使用 compile-web02 原二进制和新的 dist-02，原 Go 构建输入未变；旧 2788 项保守编译 manifest 中 Web 差额按原记录区分，不称全部相等。
- native02 的 normal 为 1 Create、2 Update、0 Lookup，26 请求中 24 finished、2 个 GET 保留原 ERR_ABORTED；lost 为 1 Update、1 Lookup，17 请求中 16 finished、1 个原 PATCH 真实截断。43 份联证、5 张截图及实际资源全尾有限通过。范围是 runtime-off 默认 App 的配置闭环，不包括物理 Runner 执行或 Execution 开放。

## 有界源码恢复

以下补丁仅用于**隔离目录**，禁止应用 main。默认恢复从 main `94a225a2` 起；`20160904` 是其后的一层。后三个早期补丁各自从全新的 main `94a225a2` 起，只恢复表列两文件，不是完整旧工作树，也不能把所有补丁顺序叠加。

| 补丁 | 应用基础 | 已保存的历史差额 |
| --- | --- | --- |
| [retired-inputs.patch](retired-inputs.patch) | main `94a225a2` | `50d86cef` 的 4 个 Web 文件：Create/Edit form、useProjectAgents、专属 page spec；对应 native01 与 compile-web02。`14562113` 的 Web 源也与该版本相同。 |
| [retired-inputs-20160904.patch](retired-inputs-20160904.patch) | 默认补丁之后 | 原 compile-web01 唯一 Go fixture 的未用 `config` 局部变量。 |
| [retired-inputs-e1eede46.patch](retired-inputs-e1eede46.patch) | 独立 main `94a225a2` | 早期 sparse 输入的 API codec 与 client spec。 |
| [retired-inputs-52da33df.patch](retired-inputs-52da33df.patch) | 独立 main `94a225a2` | 当前拒绝清理修复前的 useProjectAgents 与 state spec。 |
| [retired-inputs-60fe4c7e.patch](retired-inputs-60fe4c7e.patch) | 独立 main `94a225a2` | Usage 组合前的 client 与 useSession。 |

在隔离恢复目录使用 `patch --batch --fuzz=0 --forward -p1 -i <补丁绝对路径>`。五个补丁均已实际应用并 Wait 0，无 fuzz/offset，结果逐文件等于命名历史 Git source。默认和 compile01 补丁的变更文件 SHA 匹配原 compile manifest；两个编译 manifest 中各 2706 个 tracked source 的 Git blob 身份也已闭合。原件见 [restore-checks](../../output/ai/retired-20261011/agent-runner-mounts-ui/restore-checks/) 与 [source-closure-check.json](../../output/ai/retired-20261011/source-closure-check.json)。

对 `e1eede46`、`52da33df`、`60fe4c7e`、`14562113` 中 17 个已知 authored Web 路径的 63 个存在版本，已逐项确认来自以上补丁或可达 main `94a225a2`、`33591391` 等命名源，见 [ui-known-source-aliases.json](../../output/ai/retired-20261011/ui-known-source-aliases.json)。这是有界 Git blob 对照，不是全树归档。

**历史限制保留：**早期若干纯控原 invocation 只记录“某 ref + bounded UI diff”，没有完整 source map；原 result 也没有保存未提交差额的全部字节。现存日志与已知已提交文件版本已保全，但不能回溯证明这些未记录 dirty 输入，不能声称完整重现早期原失败。本次没有补造 source map、重跑测试或改写原结论。

## 路径与依赖

原配方仍记录原树绝对路径。日后恢复需显式建立隔离源码及依赖并映射保全路径，不能直接将旧记录冒作新运行证据。旧 web/browser `node_modules` 是同锁可再生依赖，不额外复制；SystemRunner 两个作者已确认各自物理私有复制完成、无旧树运行依赖。原 owner 无 writer/后台；原树及可再生依赖尚由主线程决定普通清理。本次没有 Git 写入、删除工作树/分支、Go/native 或网络动作，也未触及受保护缓存。
