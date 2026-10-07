# Project configuration writes HTTP rev2：差量及完整限定 STATIC

**PASS，零未闭合阻断。** 冻结 draft-rev2.md SHA dbfa7d63510e8d292b2a803c0e0b3e75492a9d6bae3a9758887aa2c276400eee；复用 rev1 完整独审已通过部分，本次独立核 B1/D1、计数/索引联动及无额外技术漂移。结论仅为 scratch 工程规格，不是产品或动态验收。

B1已闭合：唯一新增技术#14 tests/model/project_configuration_http_root_test.go，只允许旧 Owner POST models 的 nil/405 改为空JSON对象/400 InvalidArgument。固定helper在非nil body时提供正式Content-Type/CSRF/key，故此例可进入新严格输入层；不改helper，不删除旧top，五读、Update历史、ready/init/drain/restart与日志断言全部保持，并继续要求本卡实际跑旧root。

D1已闭合：跨User/Project明确为不能串用identity/receipt，不再误称不能串行。#1–13与旧候选逐对象相同；README只由#14顺延#15。总15路径=14技术+README，10新5旧。新#14接受源已在原45固定输入中，无新业务依赖或根写权限。

实际两稿diff与冻结patch c8c6f3d964adbaf42829eb1a8ee09a1b2c62a0f523d91db6495d4cbcfb934f33 完全相同；逐项审定11处替换和1个B1段后逆向还原，逐字节等于原rev1。输入清单仅追加修订/反馈/索引，既有源不变；全部旧路径源指纹和B1私有提案指纹一致，提案确为单行差量；11链接有效、格式无漂移。checks.json SHA 0a91bc1d4b51e76cdae46531f11b036797d715c87ba43cd523c8ff6a39605838 记录实际静态核对。

复用原完整审查：../project-model-configuration-http-spec-static01/review.md SHA 17a65c5e499e8133f728f3528061fa3f2bf92426d0a1a2639a881117ecf4de67；原rev1 NEEDS_REVISION与发现原件保留。typed/policy分界、历史expected+1、原ctx私有确认/公开EX lookup、新写后坏receipt私有Unknown与只读坏union NotStarted、I/O收尾/root组合与既有限制均无额外变化。

实施仍须凭据完整产品（含README）独立接受、root采纳和实际根依赖移交，再按最终基线冻结15候选/实际图并另授；此稿未正式归位。未读活动凭据源，无Go、测试体、schema产品验证、socket/PG/资源、正式产品文档或Git操作；无活动命令资源，完成后停写。
