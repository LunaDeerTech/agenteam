# Independent query controls

PASS，限定 query01 受控查询补集。普通/race各 1top/9sub，全部 actual0；没有 Go 首红。八条实际命令（format、两图、两compile、list、两run）每条45s以内，编译/运行分开，subreaper已验证、direct child实际wait、无需额外强制清尾、owned两次为空、前后有效输入一致。各轮原command/result/stdout/stderr保留在result.json索引。没有native/PG/监听资源。

普通267pkg/1645file，race269pkg/1651file，均generated testmain1/extra0；实际本地GoFiles无init/TestMain。图读取并绑定旧GoFiles/CgoFiles/EmbedFiles及标准variant，生成main完整内容及hash保存在actual-graph.json。继承已冻结工具/模块指纹后按实际图缩窄为input01；实际运行另绑两binary的binary-input01。未复制整仓。

15项overlay：#1固定query01；#11固定接受cc850b22 account；其余12作者卡路径删除；增加私有1test。hash读取和package-file-set检查采用同一有效overlay，未来作者新文件仍被隐藏，不读其活动字节。本轮query包没有依赖HTTP/app，也不把其隐藏版算成新根已验。

补集：同Tx UserSH→ProjectSH→当前Session→OwnerRead→Query和真实回调终局；当前Session/Owner明确拒绝且零查询；Unknown原result State/AttemptID/Cause经errors.As多层包装恢复，内部CauseID/RetryHint保持，只有一次WithinTx，不接受无原wrapper的Unknown；callback完成后主动cancel，持有真实tail时同步调用不提前返回，Cleanup无条件cancel/release/actualdone等待；limit200真实构造201rows、坏201filter、202合法额外row、Close后才有Err全部全行检查且失败零items/cursor，合法页cursor由200th返回行生成，换limit1续同时间ID边界仍合法。

这些是受控Store/authority及私有recordPage+projectPageCheck组合断言，不冒物理PG锁/正式Owner/真实COMMIT证据，不冒公开ListProject的真实数据库成功页。正式当前Session/Owner锁竞争与terminal-first ACK丢失、完整HTTP/31metadata/schema/真实最大页/默认根/native均另待完整freeze和资源授权。production01五源的独立STATIC结论另列，未扩为动态整卡PASS。

本私有补集已冻结停写。产品没有修改。
