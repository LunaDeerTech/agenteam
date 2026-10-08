# D11 Work structure 规格验证

root 已接受 [正式卡 rev1＋D1](../work-items/d11-work-structure.md) 的完整 SPEC：rev1 全文独审发现唯一 D1，精确修订后最终组合 PASS。**这是规格接受，不是产品实现、编译或真实 PG/资源验收通过。**

| 固定阶段 | 原件与结论 |
| --- | --- |
| rev1 候选 | [完整卡](d11-work-structure-spec-verification-evidence/originals/author-rev1/card.rev1.md)，59476B，`6d4f3335`；[作者 freeze](d11-work-structure-spec-verification-evidence/originals/author-rev1/freeze.json) 保留全部小件指纹。 |
| 全文独审 | [原 FAIL](d11-work-structure-spec-verification-evidence/originals/independent-rev1/review.md)，`fdd314b3`；[evidence](d11-work-structure-spec-verification-evidence/originals/independent-rev1/evidence.json) 记录六组工程契约、18 路径、6 新 PG top、7 旧 pure/5 旧 PG 与两种独验构造的静态覆盖，唯一必修 D1。 |
| D1 修订 | [单行差量](d11-work-structure-spec-verification-evidence/originals/author-d1/rev1-to-rev2.patch)，`90a53acd`；[修订卡](d11-work-structure-spec-verification-evidence/originals/author-d1/card.rev2.md) 59597B，`c07c7112`；反向精确恢复 rev1。 |
| 最终组合 | [独立 PASS](d11-work-structure-spec-verification-evidence/originals/independent-final/review.md)，`febcf667`；[evidence](d11-work-structure-spec-verification-evidence/originals/independent-final/evidence.json) `ea9760f2`、[manifest](d11-work-structure-spec-verification-evidence/originals/independent-final/manifest.json) `bca8acb9`。复用 rev1 全文审查，仅复核 D1 差量与最终组合；root 已读核接受。 |
| 后端消费 | [准备](d11-work-structure-spec-verification-evidence/originals/backend-preparation/handover.md) `8dd31825` 和[原 rev1 消费](d11-work-structure-spec-verification-evidence/originals/backend-consumption/consumption.md) `e99a042e` 保留当时边界，不冒作独立审查或实施通过。 |
| 行政末件 | [五处差量](d11-work-structure-spec-verification-evidence/originals/administrative/administrative.patch) `e4ab74b5`、[反向自查](d11-work-structure-spec-verification-evidence/originals/administrative/checks.json) `f021f46d`；[固定快照](d11-work-structure-spec-verification-evidence/originals/administrative/card.administrative.md) 60230B，`812490b6`。root 亲自反向得到精确 c07 并接受；产品契约不变。 |

D1 只纠正 §8.4 继承预算的真实含义：启动前 fresh 可用磁盘≥5GiB；owned/resource cleanup 后的补充 host TCP delta 尾部两次清空观察总限≤75s。后者不证明完整短连接轨迹或 tuple ownership。原来的 Memory 上限/完整 TCP 暴露窗表述保留在原 FAIL 中，不再适用。依据直接复用[既有永久 driver](project-owner-ui-recovered-read01-verification-evidence/originals/run/driver.py.txt) `bb666f55` 的419–438、490–494、657行，不重新复制或执行。

接受语义输入完整 SHA 为 `c07c7112a5fd77bd475cd843fbffc9680b030b626550632f2999d849476bf07c`。行政卡为 `812490b67b956469e274eecfde851eb084e47648fe0e3944eb5668fa8961c27f`；其五处仅更新规格接受、唯一作者和授权阶段，§2–8 除 §6 授权说明外逐字不变，18 路径/六命令/五表/读取与错误顺序/锁与事件/恢复与后继边界保持。

本档封存时，root 仅将 #1–17 移交 fixture_recovery 在自有 scratch 形成完整实施候选。行政卡 STOP 后另有一次固定 local scratch gofmt≤45s 的窄授权，不占 build cache；这是后续作者授权，SPEC 作者没有执行或验收该命令。main 安装、编译/vet/list/testbody、Node、真实资源及 README #18 均仍未授。00021 仅预留；Task membership/删除正向、Sprint 生命周期与 pointer writer、Work 整体清理、Execution/Dispatch、HTTP/Tool/root 和生产 ready 仍未绑定，不能以 empty 冒充依赖完成。三项硬停止不变。

有限永久档含 34 件原始小件共315325B，包含最初前沿候选、完整 rev1、原 FAIL、D1 修正、组合 PASS、必要作者准备/消费和行政反向证据。[source-map](d11-work-structure-spec-verification-evidence/source-map.json) 给出原路径、全 SHA、长度与永久相对路径；[manifest](d11-work-structure-spec-verification-evidence/manifest.json) 和[归档核对](d11-work-structure-spec-verification-evidence/archive-checks.json) 绑定生成件。38 源指纹和必要增量只保留原索引；不复制这些源码、旧 driver 或完整 Go 图，不重做动态检查。原始文档内的临时路径和相对链接保留原语境，由 source-map 映射，不改写原件。

[格式例外](d11-work-structure-spec-verification-evidence/format-exceptions.json) 逐行登记原 patch 的真实 context 行与 EOF 空行，原字节不归一化；新报告及元数据格式干净。归档者仅做字节/引用/差量/格式核对，未执行 Go/Node/Git/网络/业务资源，未修改计划或业务源。文档末件 STOP，提交与后续授权由 root 处理。
