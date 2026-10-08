# D11 Task 规划规格验证

root已接受[正式卡rev1](../work-items/d11-task-planning.md)的完整独立SPEC，结论为 **SOURCE_SPEC_STATIC_PASS，无必须修订项**。这是规格接受，不是Task产品、迁移、编译/测试、真实PG或完整D11验收。

| 固定阶段 | 原件与范围 |
| --- | --- |
| 完整rev1 | 59457B / 367行，SHA `6f1fb8575861a8db1be38862a6c9936b106bd7b4b9225515e21db35a92996108`；作者freeze `c54e2d8e`固定10件，26个必要来源在正式卡§10和永久证据保留 |
| 独立全文审查 | review `06ebfcb09aaba9ecdf90edd22f6d9441fa37543458e9662772ffe4a4590aae6d`、evidence `080a7cec`、manifest `bf3d4f84`；通读全文并核公共接缝、selector声明、容量上界与两处行政反向，不是只审局部或实际运行 |
| 作者接手准备 | handoff `1208ec8c`、freeze `63882433`；只读可实现性，未发现必须扩写域的确定阻塞，不作独立验收。原记录的首hash输入误录及后续正确核对仍保留 |
| 当前行政卡 | 只新增一段当前接受/授权说明，SHA `361c4dfff975fe25e5f8790a0b6f64d08013b51c50f61e0700a7d18f3318ab95`；移除固定插入段精确恢复原rev1，§1–10及初始阶段原文逐字不变 |

独审覆盖Human未指派backlog的完整规划结果：显式type/priority、严格presence/Unicode与最坏JSON cap；当前读、assignee三态filter、分页query generation；caller同Store活Tx、真实placement和Task membership；当前授权→原writer/semantic/历史receipt→新写门槛；原始锁union先限512再Normalize、共享三轮计划、U1取消/Unknown原writer因果及Stop/Drain真实join。首次业务变更就要求同Tx的TaskEvent、typedOutbox、receipt与Activity；旧两Work分支和Project精确gate边界保持。

21拟产品路径为20技术＋README末件。00022仅由root唯一预留；旧fixture继续迁移最新再核原五个命名表，历史Structure Migration固定through00021且保DDL/约束断言，新Task Migration另验fresh/populated00021→22。七新PG、八旧PG、七个Project pure代表和独立A/B只是已冻结验收要求，未据此声称任何新selector已编译、发现或通过。预算仍为PG两ID、105+15/120秒含setup/cleanup、package≤6m、fresh可用磁盘≥5GiB及cleanup后补充TCP尾部两清≤75秒，各层Wait/join与外部工具终态分别记录。

本档时点，root另授fixture_recovery仅在自有scratch推进20技术路径，当前先六源。main安装、Go编译/测试、真实资源及README #21仍未授，后续窗口由root单独移交。规格作者没有执行或验收该实施；Agent/assignee、状态/reviewer/Blocker、Execution/Dispatch占用与历史、Task删除/Sprint lifecycle、Work清理、完整Timeline/HTTP/Tool/UI/root仍未绑定，不能返回假空或默认成功。三项硬停止及Image/Jina边界不变。上游[Structure全18接受](d11-work-structure-verification.md)保持原版本组合及独立B外部工具terminal缺口，非完整D11或一次当前HEAD全测。

[永久小证据JSON](d11-task-planning-spec-verification-evidence.json)内嵌9件原始小记录共42521B的UTF-8原文、完整SHA/长度/来源路径，以及本次单段差量与精确反向数据。原独审/source记录无需依赖scratch仍存在即可复核；源码本体、完整Go图和整份规格不重复复制，原rev1可从固定行政卡恢复。原件内临时路径保留原语境，不把其声明的读取或执行范围扩大。

计划 `75fc145c` 与Structure卡 `d99851a9` 的两处已接受头部本次未改。原patch和本次patch仅有格式标记所需的空context行，永久证据逐行登记line-kind；原字节不归一化。归档仅作小件字节/引用/差量/格式核对，不是再次独立验收，未执行Go/Node/Git/网络/资源或读取活动实现。文档STOP，提交与后继授权由root处理。
