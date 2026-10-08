# candidate02 差量及 integration 离线提案

**PASS（STATIC／离线准备门禁）**。复用 candidate01 完整五源审查与 unit04 作者 pure 证据；唯一 IC-PG-01 已由新 fixture 差量关闭。新候选 manifest `95b8d89d52703d90215019dcb7cedcfd422ba8e5f3287e181b496d70d9acfd53`，integration-overlay02 freeze `65808a912a30f697ae616284f6dfc3720751d150ae4fe093bc517eb5c9a4f40a`。

fixture 首次 Drain(2s) 的 context 在返回后立即 cancel；失败仍报告原 error，再调用并取消 ForceClose(1s)，保留其 error；最后 Background Drain 实际等待 Store operations/targets 与 pool/socket关闭。固定 cancellation work 持有 target 引用，不能通过退出 Force 隐藏未join的本地工作。该最终等待属于原120s top含Cleanup的所有权范围，不延业务预算，若不能完成由后继资源driver判失败/收尾；现在并无动态终局声明。其余四源逐项hash不变。原 candidate01/finding 原件保留。

静态核精确8条argv：两PG源gofmt-d预览、full20 race integration实际list图、dynamic三helper＋两cmd普通list图、Project integration race compile、精确新2旧2 binary list、race vet、两cmd build。无test body／app全包／fixture脚本执行。格式非空必须保原raw并另冻布局候选，不可覆盖candidate02。普通/race纯组不重复。

7 overlay映射为五scratch新增源加两UI Go删除，input与实际overlay逐项一致。只枚举UI路径名，不读取内容/指纹；固定文件表不包含两删除源。2977文件SHA与415有效目录集合逐项当前匹配，未用旧图冒新actual图。原Audit接受full20/dynamic图及输入指纹相符。collector按GoFiles/Cgo/汇编/embed等有效物理源解析，遇两删除UI或web源立即拒绝；extra非0不能产出build input。所有生成main纳指纹，Project main要求无_test/_xtest.TestMain方可-list；固定14个既有Project Go文件无init/TestMain，新两测试亦无。其他包main仅list记录不执行。旧CGO0图复用只适用于逐字节相同、无新Project依赖的后继资源方案，不是当前新图通过。

driver相对已验pure版本仅增加有效目录集合overlay增删与更换固定GOFLAGS overlay路径，保留Go1.27.1/offline/modreadonly/p1/GOMAXPROCS2、42s干预/45s整命令、subreaper实际direct/adopted wait、双owned空、前后inputs同及原红保留。collector/build使用不同阶段input，必须等两实际图终局PASS/extra0/新generated main固化后才编译；binary指纹随后绑定再精确-list。collector本轮仅读代码，没有执行。

此结论允许root审后授这些必要离线命令，不代表它们已运行、PG语法/锁竞争已通过，也不授真实资源。候选仍仅scratch，未安装仓库。未来full20/7资源/live基线/实际120s含Cleanup watcher另冻交窗；UI资源不得重叠。没有读取活动integrationoverlay以外的未冻方案，没有Go/资源/Git/产品改动。已停写。
