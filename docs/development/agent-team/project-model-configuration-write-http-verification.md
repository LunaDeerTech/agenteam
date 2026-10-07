# Project Model configuration write HTTP 完整限定验收

**完整15路径已独立 PASS 并由root接受**，产品 `cc850b2244cad771eb862a2c82d99887d5da7284` 已推送且root核远端一致。范围为14技术与后端README末件；[最终15结论](project-model-configuration-write-http-verification-evidence/originals/independent/final15/result.json)承接[完整14技术结论](project-model-configuration-write-http-verification-evidence/originals/independent/final14/result.json)。具体行为见[正式工作卡](../work-items/d09-project-model-configuration-write-http.md)，源码复用产品提交，不复制另一棵源码树。

接受当前Project Owner六个Provider/Model写入和被动found/receipt lookup，与既有五组GET/HEAD默认根组合；1 MiB严格typed输入、1 KiB安全输出、原Model当前Read→历史→新Mutate、原context私有确认、公有Command EX查证和30秒写/2秒lookup发布I/O预算均保持既有契约。历史命中不绕当前身份，预算不承诺每个handler按时返回；错误、Close/callback实际尾部及被损坏写receipt的Unknown与被动lookup的NotStarted分开。Project引用仍消费已绑定adapter，不冒新增Agent/ProjectSummary绑定。

## 有效版本与原失败

生产四源自production01未变；candidate01–06继承manifest与最小测试delta保留，最终effective14逐项匹配冻结快照。作者Model普通/race各4 top/137 sub、app普通/race各3 top/62 sub及encoder schema8样本/40负例、两cmd构建和实际图按未变输入复用；末次受影响racecompile/精确13名list/vet通过。独立controlled普通/race各1 top/14 nested通过。compile/list/图检查不冒真实测试；不宣称所有项目在最终候选重新全跑。

| 原记录 | 修订与观察边界 |
| --- | --- |
| candidate01准备期生命周期改变不足以证明终局Project gate rollback | candidate02增加真实NewFact成功及合法foreign issuer真实Forbidden、同事务canonical/ref/Audit/准备命令证据。 |
| native writeclose首次FAIL | 原合并判据未区分ReadAll错误与Problem字符串。candidate03只修测试观察；重跑见合法405部分8192 B头/EOF、Write0/Flush1/nettimeout，不能称实际完整头后零body分支已触发。旧blocked_get_write标签实际POST lookup。 |
| crud01首次snapshot DATABASE_SQL_FAILED | candidate04仅修models经provider JOIN；原raw无具体列或SQLSTATE，静态缺project_id归因不倒填成动态细分。crud02为单组实际PASS。 |
| authority01登录/注销后全snapshot不等 | 原raw无bucket/行差异。candidate05区分System Account setup refs与拒写基线，桥接五Model桶及精确Model-owned Secret引用，再用完整新snapshot验证拒写；未削弱全refs比较。 |
| authority02辅助archiving准备失败 | candidate06复用正式BeginArchive和独立Project/本scope凭据，满足operation/FK；原raw无具体PG约束名。Archived辅助终态仍不是participant运行/完整Archive。 |

原完整STATIC曾漏fixture SQL/Account基线问题，保当时结论及后续修正。私有独立A的同类基线问题在执行前静态发现并修正，不伪造独立失败轮。原图后处理误断言、缺OperationID建议的撤回、A启动前未spawn的transport中断、metadata字典/字段误取与root短只读transport问题分别留原说明；均不冒产品/Go失败。

## 真实验证与资源

作者native有4实际轮（keepalive、slowbody、writeclose FAIL、writeclose重跑PASS），独立native再1轮；共5轮direct5/adopted5实际wait，首红direct exit1保留。三组作者通过结果为版本组合；每轮owned PID与全部TCP（含TIME_WAIT）双空。

作者9实际PG保3 FAIL、6 PASS，最终五新＋八旧13 top/35 sub；独立A/B再2轮PASS。[逐轮原件与差集的派生核对](project-model-configuration-write-http-verification-evidence/derived/resource-crosscheck.json)由11份原result/cleanup解析：每轮7资源actualwait、watchdog join、双清及输入一致，按ID实表去重得77个不同ID；284→328的PID/starttime集合连续，最终差集恰为44个新daemon/PID1 shim（作者36＋独立8）。它们非owned、未task-wait，历史git/go/compile另列，不称全机清零。所有本卡窗口已归还。

独立A补全Model引用索引，不能用作者Secret refs替代；实际先C(COMMIT)+Z(I)再丢ACK，核原Store Unknown/cause/attempt、原context私有确认及后续显式EX lookup/正式Session恢复。作者held-writer先断client再转发并观察终态为另一时序。lookup-read-unknown是实际读取后受控terminal替换，不是物理读ACK丢失。独立B沿真实默认根验证当前读写、历史receipt、归档重放/新写拒绝及正常关闭重启；forced app返回与所有inner join不等价，client/proxy/原Command EX终态、owned wait另列。

[作者原body索引](project-model-configuration-write-http-verification-evidence/originals/author/body-index06.json)中本卡18份，加独立4份，共22份原字节/旁车与三批实际标准schema输出逐项hash多重集合一致，未重编码。作者旧回归域13份单列，不混入新配置覆盖。完整末件README只作STATIC和58链接核对，没有重跑技术或资源。

## 保存范围与限制

[来源映射](project-model-configuration-write-http-verification-evidence/source-map.json)绑定309个去重原件、175个同字节别名、3项已提交共享脚本复用、133份元数据的6个明确派生束和17项仅指纹来源。保必要命令/raw/失败/delta/实际wait/清理/原安全body及独立probe；重复大图、初始全文差量和README全文复用固定来源，不复制cache/binary/private runtime，不声称完整依赖树已存。

归档核原件字节/来源hash、JSON、脚本AST、新链接及卡§1–9和入口旧历史原字节。19份原件有184条空白扫描记录（尾空白53、space-before-tab 129、EOF空白分类2），另70份原body/旁车无末换行；逐行记录见source-map，不改原字节、不ignore，实际Git检查由root执行。本次无Go、资源、Git或原证据脚本执行。

系统管理员统一会议Summary initial/update含首轮标题，Project不override/复制初值，compaction/Execution Summary不改。配置UI、生产Resolution/Invocations、真实Provider调用、D24及未绑定引用adapter未因本次接受而完成；ready503、D08–D28/E01未完、E01未开始及Object runtime join/OpenAI tools独立验证/SPA concurrent-publication三停止保持。
