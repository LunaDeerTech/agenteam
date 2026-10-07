# Candidate01 + Candidate02 app delta 独立静态阶段结论

状态：**有界生产 STATIC 未见阻断；整体验收 HOLD（新测试缺陷与必要组合覆盖待补）**。不代表编译/动态通过。

输入：candidate01 manifest `2fee5d4b066f8d8e78ca6dbd2b095ddd12ac0038ea9a12c95ca54e6dddd7800e`；candidate02 manifest `787e494ce78c3b1450680e725f68a287a4ab73308516a6e7d37c3bf25dc7208c` 仅两app源typed ProcessID adapter与测试静态接口修复，其余16同01。18快照hash已逐项核对。固定产品901eb546，规格03d1c107。只读冻结快照，未读返修中的活动测试。

生产：严格请求presence/key/version/目标/方法/unsafe POST CSRF沿真实Boundary；Update/lookup只有一次同步调用；原3s观察尾不延长30s/2s发布期限；旧requestIO Close/callback/Flush所有权复用；安全历史active Project/版本关系和update-only union再次校验。根前置同一纯createProjectUsage，真实Audit/Outbox双Project gate及typed catalog先于Seal，新Service立即登记accountAssembly；Project work先Drain且真实nil才Joined，Force不首错跳过后续provider。candidate02同真实process字节转换到Outbox typedID并通过原Account adapter委派guard，无公共口扩大。新schema旧GET/HEAD操作及所有原schemas结构完全保持，新增refs均本地。

明确问题已即时报root并由root协调作者补测：

- U01：`project_owner_update_http_root_test.go:79` readyz用`.want(t,503)`，固定systemHTTPResponse.want要求Account安全头，诊断路由无此头。应只改新测试直接status/Problem，不改生产/通用helper。
- U02：`project_owner_update_http_unknown_test.go:20–113` 两相真实ACK丢失有效方向，但只是即时确认；缺原WithoutCancel确认被阻塞时parent取消/Stop、实际Service Drain未终局→放行actualwait、确认中撤权。`root_test.go:82` 只idle stop，不能证明默认root在途Unknown确认/耗尽预算。`app/project_update_test.go`的非合作WithinTx lookup是controlled生命周期测试，不是实际Unknown尾证据。卡§4/5.2/7新增组合须补。
- U03：`fixture_test.go:48–74` gates真实委派+计数可证明valid CurrentAccess/NewFact路径到达，但reject标志只在真实成功后人为Forbidden；`http_test.go:127–137`因此不能证明实际validator拒绝bad cause/changed_fields/summary/prepared plan或缺锁。需真实Audit坏事实及Outbox实际heldlock/事实拒绝代表，保canonical/receipt/Event/Touch零副作用、到达而非构造提前拒绝证据。

复用限制：旧B02双planned命名竞争、原子事务与原writer commit/rollback/attempt验证可按固定字节复用。旧ReceiptRequiresCurrentSession实际主例为Create+SQL身份，不能替代新Update正式身份链/当前事务授权。自然30s/2s、native及最大合法编码当前只读到源码，未动态采纳。标准schema合成正反例不等于真实响应原bytes，独立B拟承担真实同body解析。主体身份新fixture复用正式Bootstrap/Invitation/Redeem/Login；辅助Skills/lifecycle/testProcess不能当默认root生产绑定。

根已授权补测为私有精确backend PID proxy：区分收到尚未转发COMMIT的pending原writer与放行后实际server终局/响应丢失；不得用结果装饰。正常root/耗尽root退出与proxy/backend等待分别记录，后者不得倒填root inner join。等待新freeze后只审受影响差量；不动旧raw/原首红。

此阶段未执行独立Go/Node/标准schema动态/native/PG。root已另授必要离线；将先隔离固定HTTP包私有probe，真实资源仍未授。
