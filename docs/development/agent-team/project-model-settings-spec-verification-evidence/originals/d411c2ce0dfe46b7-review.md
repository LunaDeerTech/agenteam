# Project Owner 模型设置整合候选 v01 — 独立 SPEC STATIC

**完整候选 SPEC STATIC PASS，无必修，STOP。** 只接受 scratch `candidate.md`（84300B，SHA `c5674d4e06618f264a00867de0fb65fb9710e56b6a96ccde1f8fdaa77a5641f3`）的组合规格，不是正式卡安装、产品实现或资源许可。冻结为 `dedc14db…`。

已通读全部 731 行。复用 rev0 `463cd09a`、rev1 `691ab122`、T3 `0e60d1ae` 的独审；独立文本对照确认原 §2/4/5/7/8 整段字节保留，完整恢复矩阵未删减。rev1 两段 typed 代码原样纳入；T3 §1/2 正文原样，§3 仅将 endpoints 引用改为精确路径。运行协议保留的14组字段与原值相同；被精确 DTO 替代的旧抽象 counts 没有并列成另一契约。

29 个唯一路径＝26 前端技术＋2 新 Go＋README 末件；17 个 endpoint/action 一一对应 6 GET、9 mutation、2 POST lookup，9 IPC 不扩。6 新 top 与14旧精确 selector 对应固定表；新私有 dist、Owner/Audit/Summary 私有 reader 与其余 global reader 分列。45s case、120s top 含 cleanup、6m 包、75s TCP、5GiB、7ID、单 worker／零 retry 保持。独立至少两轮不同构造仍是真实后继要求，未用作者日志替代。

恢复与权限没有冲突：完整 Problem 的首次拒绝闭集、Unknown 粘性、KeyReused、损坏 receipt、删除后原请求、明确放弃和确认后读失败仍分别处理。两 lookup 只观察历史，不能确认输入／Credential 材料；归档后配置原 Execute 与 Credential 仅 lookup 的差异保留。Current GET、快照提交或同 User 的新 Session 均不能替代原 Execute 或保留跨身份材料。

完整读 DTO 与较窄写 policy 分开；全 Project Models 不加 provider_id，不用首 page 判 Provider 无 Model；可用目录七字段不补 System Get。Credential 独立确认后只保安全 ref，由用户另存 Provider，没有自动链式写或补偿删除。完整 Session identity、普通 Owner 显式分派、稳定 Project ID、实际 Cookie owner 尾部和旧新草稿聚合确认均保留，workspace 读归属没有升格为写授权。

私有协议的材料、安全内存、原始请求比较和 actual join 边界一致。origin／request token 配对不等于命令同义证明；未完整比较为 null。Go joined、浏览器 native EOF/client/schema、Cookie owner finally、只读 Tx 提交分列。前后端 agreement 确认同一 T3 SHA 的可消费性；三 tuple 只说明 Recovery 代表可排布，不替代完整矩阵。最多4 origin／每 tuple一个仍约束未来六 case，不能覆盖 origin、删场景或自动扩预算。

Audit 21条元数据与其中17源 build元数据相符，只作版本引用。**Audit 整卡、T1 最终共享源码／main／签名与 import 交接、T4 正式卡安装和唯一写权仍 pending**；实施后的工具／源码／schema／dist 闭包、discovery、driver 与逐轮资源许可继续另行固定，README 待28技术接受。它们是准确保留的交接项，不是本候选新必修，也未被本 PASS 预填完成。

仅核22份固定文档／元数据引用并做本地文本、JSON与哈希对照，未扫描业务源码或大图，未运行 Go／Node／browser／资源／网络／Git，未写仓库。一次错误 protocol 文件名的只读失败及其更正保留在 evidence；没有产品行为检查。原草案、FAIL、pending及此前 Audit 结果均未改写。
