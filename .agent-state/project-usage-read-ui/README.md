# Project Usage 只读 UI 历史材料恢复

原工作树 `/workspace/agenteam-project-usage-read-ui`，最终 tip `c085e72037257c97452a6e7435e201d2818be0bc`；正式交付基准为 main `066bf03c`。22 个技术路径中 20 个逐字相同，两 harness 的差额是主线保留其他已交 profile 的并集。本目录现有 launcher/profile controls 未改。

## 原件位置与范围

2026-10-11 将原树 `output/ai/project-usage-read-ui` 整目录以同文件系统、禁止覆盖的 rename 移至 [保全目录](../../output/ai/retired-20261011/project-usage-read-ui/)。原 `web/dist` 同样移至保全目录 `original-web-dist`。两次迁移时 inode、mode、uid、gid、mtime 保持；具体原→新映射见 [preservation-record.json](../../output/ai/retired-20261011/project-usage-read-ui/preservation-record.json)。

`/tmp/puw01`、`/tmp/puw02`、`/tmp/puw03` 原目录保留，小副本分别在 `original-tmp/puw01`、`original-tmp/puw02`、`original-tmp/puw03`；各文件字节、模式与 mtime 已核相等。保全目录保留全部现存前端失败、方法反例、编译/list、原 binary、82 个 dist 资产、三轮 native、截图、输入 manifest、实际 Wait 与资源尾记录，未重写原路径或原结论。

native01 原 Host 投影缺失 FAIL、native02 窄 Drawer 未打开 FAIL、wire-retire-repro01 两个反例 FAIL 均保留。native03 五次 List、三图、8 GET 的有限业务与完整退出尾接受保持：6 finished，另两个 Usage GET 的原 ERR_ABORTED 及完整消费联证保持。此范围不包含生产 Runtime producer、summary、估价、Provider 调用或完整 D09。

## 有界源码恢复

从 main `066bf03c` 建立**隔离恢复目录**，按表顺序恢复所需历史阶段。补丁仅用于原输入恢复，禁止应用 main；默认补丁中的历史删除也不是当前产品变更。

| 补丁 | 应用基础 | 恢复用途 |
| --- | --- | --- |
| [retired-inputs.patch](retired-inputs.patch) | main `066bf03c` | 最终 `dab84c8c` 的原两 harness；另删除当时尚不存在的 9 个 internal Outbox/Runner Workspace Go 文件，使原保守 compile 输入集合保持原样。 |
| [retired-inputs-f46214a5.patch](retired-inputs-f46214a5.patch) | 默认补丁之后 | native02：恢复唯一 spec 的窄菜单动作修复前版本。 |
| [retired-inputs-6ebb0030.patch](retired-inputs-6ebb0030.patch) | 上一补丁之后 | wire-retire-repro01：恢复专属 helper 与 profile control 的原晚到事件反例版本。 |
| [retired-inputs-70cd66ee.patch](retired-inputs-70cd66ee.patch) | 上一补丁之后 | native01：恢复专属 helper/control 与共享 helper 的真实 CDP Host 观察接入前版本。 |

只恢复 native03 时用默认补丁；native02 用前两层；原 retire 反例用前三层；native01 用全部四层。执行形式是在隔离目录内使用 `patch --batch --fuzz=0 --forward -p1 -i <补丁绝对路径>`。四补丁均已在隔离临时目录实际应用并 Wait 0，无 fuzz/offset，恢复文件逐字等相应原 Git source；有原 manifest 的变更文件 SHA 同时匹配，见 [restore-checks](../../output/ai/retired-20261011/project-usage-read-ui/restore-checks/) 与 [restore-results.json](../../output/ai/retired-20261011/project-usage-read-ui/restore-results.json)。

命名 main 基准加上述补丁链，已通过 Git blob 身份对照覆盖三轮 native 各 1939 个 tracked source。原 compile 的 1715 个仓库源文件及其保守扫描集合一致（第 1716 项是外部 Go 工具），两个 wire-retire 原控各 8 项 SHA 也逐项匹配。见 [source-closure-check.json](../../output/ai/retired-20261011/project-usage-read-ui/source-closure-check.json)。这不是重新运行原验证，不将外部工具、依赖和原运行的全部 2555 项输入冒作 Git 源码。

## 早期输入限制与依赖

早期 client/view/type 目录实际保有 10 或 11 项局部 SHA map，不能说完全没有映射。但部分当时未提交的源码字节没有匹配到已知保存版本：4 个共享文件的原 `narrow-format04` 仅存 AST 相等布尔记录，另有旧 fixture/view spec 的不同 SHA。原局部 map、日志与 FAIL 已完整保留，尚无依据还原这些缺失字节，也不能声称完整重现早期 dirty 输入；具体边界见 [early-source-limitations.json](../../output/ai/retired-20261011/project-usage-read-ui/early-source-limitations.json)。本次未补造源码或重跑测试。

原配方中的工作树、产物和临时目录仍保留原绝对路径。恢复者须在隔离环境显式映射源码及保全材料，不能直接把搬迁后路径当成原运行证据。旧 web/browser `node_modules` 是可再生依赖，不额外复制；SystemRunner 作者与 Runtime 已确认当前使用物理私有依赖或新树，不引用 Usage 旧树。原作者已停写且无后台；SkillHTTP 热 Go cache、SkillsResume 只读 modules/MinIO 均未触及。本次未执行 Git 写入、工作树/分支删除、Go/Node/native 或网络动作，最终清理由主线程另行处理。
