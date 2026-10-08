# D11 Work structure rev1 作者静态交付

完整规格已写入 `docs/development/work-items/d11-work-structure.md`；仅该新卡与本scratch有写入。状态为 **SPEC_CANDIDATE_STOP**，待独立完整SPEC/root接受，不是实现、编译或资源通过。

已按六组契约自查：数据/闭集/caps与rank、当前授权与历史优先序、分页/同Store活Tx/完整锁、命令/Unknown/取消、真实Work producer与Project精确gate、后继Task/membership/cleanup/Execution/Dispatch责任。保持18产品路径（17技术+README末件）、6命令/4读/Lookup/placement、2事件；00021仅占号。不采用机械旧输入数：必要固定来源实数为38，20个共享源码/嵌入输入在卡内逐项列bytes/SHA，其余为正式规则和精确回归定位。来源末次哈希核同，未扫完整Go依赖图。

本地静态实际检查：13相对链接/片段存在；18路径唯一且目前仅旧#13/#18存在；新6PGtop唯一；7旧pure+5旧PG selector在固定源码有声明（未运行-list）；无尾空格/tab/未闭合代码块或占位源表。格式自查用Python读取，未执行Git diff --check，因为当前没有Git授权。无Go/Node/数据库/容器/端口/网络/安装/业务检查。

原工作候选、Image两BLOCKED及三停止未改。前期只读定位有若干猜测basename/glob不存在的rg/sed诊断（包括foundation/error.go、canonical目录、identity/actor.go、旧identity/账号文档名、account/activity.go、迁移migrations.go、tests/model/configuration*_test.go）；均未形成依据，改用实际路径的局部定位。一次最初AGENTS+技能输出过长被截断，随后只读取相关治理/技能段；初始技能路径查找范围偏大，后续均限指定来源。没有将这些失败/截断写成测试通过；环境恢复前未落盘稿，恢复后root确认原writer并按0d06fd69锚续写。

已有backend只读handover仅作来源交叉核，不是独审本卡：8dd3182565d1017274f4cf8ecf95275bce6cb205e3e14ec8b275b5f44677cc30/source-refsea50b467b7d79d5246bc31231e10284d82de2a0f6cf83e2b20012851c3708ce5。作者未读取Model Settings活动源，未授实施或借其资源窗改Project/migration。

STOP后请root交未参与实现的实例完整SPEC独审；通过后再移交fixture_recovery实际实现/共享安装顺序。无当前必须询问用户的产品决定；排除的Milestone删除/跨父搬移由后继决定，不阻断本范围。
