# Audit wire01 独立审查

结论：NEEDS FIX，3项必须修正（findings.json）；五生产STATIC/query受控PASS不受影响。只审冻结#8 schema、#6 wiretest与固定production01/接受依赖，未读活动#5/#7或app/tests/model。没有Go/资源运行；独立额外运行两条45s以内标准Python探针。

- AUD-WIRE-01：四operation的405 Allow.const错误为GET，生产与卡均为GET, HEAD；同步纠正GET-only、detail trailing404及把Project/System过滤方向写反的说明。
- AUD-WIRE-02：Outbox handler_id=a\n被Draft202012接受，正式ASCII stable identifier逐byte拒绝。补严格末尾/禁用非词汇字符约束及标准parser负例，不限制原合法128字符。
- AUD-WIRE-03：Project create.accepted/completed/update/restore共享ProjectAssociations误允许operation_id。该四metadata无operation_id，正式entry与HTTP projection会拒绝；这是schema可表达的动作条件，并非不能用标准schema表示的两ID相等。保留另四动作可选关联，值相等仍由生产核。

全schema结构静审：31action oneOf完整闭合且无System输出；每record11必有/无额外属性；3Actor/13service、14resource、9通用/2model/Project关联、27reason及6consumer均与固定契约对齐。模型delete replacement分支、Project creation/operation/update/transition、Knowledge清空关联、Object3initiator条件、Artifact5source+可省略/4download phase及Failed outcome、Agent execution required均已检查。MIME统一canonical token+可选charset词汇，Progress/Version/ID/Instant复用common；跨字段ID、数字大小及canonical排序由正式生产关系校验。31metadata各oneOf required/closed属性覆盖，无新增生产阻断。页面1MiB编码门禁与静态1037420包络保持；schema的普通结构检查不冒能证明全部跨字段关系。

作者证据复核：wire-compile02→http-unit02.test二进制指纹→wire-run03输入中#6/#8精确hash匹配；actual0/5.461669696s，direct实际wait、无adopted遗留、两次owned空、输入同。其31typed representative均构造正式Metadata+Entry，通过生产projection/json.Marshal；最大已测ObjectTransferRevoke metadata1922/record2949B，200完整records+8192安全shape cursor为598220B，逐ID/metadata反解未截断。该8192token不是实际Signer产物，也不把该测试称公开List数据库201哨兵证据。79标准schema向量=31正例+31顶层extra负例+14补充Actor正例+200页+2year例，使用固定解释器、Draft202012+FormatChecker、common本地引用及20官方JSON数据；year0000/9999沿正式日历checker。其他missing/null/action cross/phase条件/MIME反例不能由79统称完整已过；我本轮40窄向量补其中代表，后续整卡矩阵仍需固定测试证据。

极值口径：598220B是已测代表页，并由1037420B静态全空间上界保证容量；当前#6的31循环并非每动作所有最大可选值的穷举（例如原Secret.resolve无可选reason、ObjectUploadFailed旧human initiator、ArtifactDownload仅sent/source execution_file）。不得用“31代表最大”冒每动作精确最大证明；正式全卡最终需要补足/映射该卡§4的分支极值验收，但不需要放宽预算/重跑无关包。

首红均保留：作者wire-run01错误cwd未到schema，wire-run02 outbound policy id误required，旧页597820B不覆盖新598220B；独立probe01私有正例误用无六位小数Instant，7正例先红，负例不可据此称有效。probe02仅修私有canonical.000000，40定点向量38匹配/2真实误接受，actual1/1.059818858s，directwait/双owned空/inputsame；其原stdout、vectors与results全部固定。无native/PG/监听、产品写或Git mutation。

建议作者仅在root授权#8/#6完成三项修正与标准parser回归，保wire01原件；业务生产不需修改。当前独立审查/探针已停写停命令。
