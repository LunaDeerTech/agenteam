# Project Owner Model Credential HTTP rev1 完整 STATIC

输入为 scratch `draft-rev1.md` SHA `cbf07235af92b97f71817d39b3e7288b01dd1836c0f6e71eea47d0d6dceb10ff`、48 来源 manifest `8efd22ca4201be434764b4ff7d5a67f9b06ea0769ef31f8b4ac9d357bcd03add`。全文已完成独立静审。**rev1 暂待 CRD-STATIC-01 工程条款精确化；其它范围 PASS，不需重审整卡。** 这是规格结论，无产品或动态通过声明。

CRD-STATIC-01（§5 85/89–91 与§8受控/native矩阵）：泛称 panic→ErrAbort 与同步Close/join尚不足以约束跨三个阶段的deadline调用。若直接借用固定Update requestIO，start setter panic触发finish后同setter再panic可跳过Close；AfterFunc内setter panic在独立goroutine逃逸；abort/reset setter或finish首次Close panic可跳后续join。建议在本卡私有实现明确每次SetRead/WriteDeadline分别recover为安全error，继续另一setter、Body.Close及已启动callback的真实join，最后统一abort；Body.Close同样安全调用、只记一次、不格式化panic值。受控例须覆盖start、callback、abort/clear deadline和Close panic，证明剩余收尾继续、无业务/晚写和实际join。

同一项同时明确两点：能力解析adapter仅交controller，认证/DecodeJSON/WriteProblem/业务发布仍走原透明writer链（或保持正确Unwrap），不得遮蔽httpapi.stateOf的code追踪与已提交防二写；安全trace冻结无载荷allowlist，例如socket/bind/listen/connect/accept/accept4/getsockname/getpeername/shutdown/close，禁止%network/all及send/recv/read/write缓冲落raw。现85行禁材料原则正确，补的是具体实现/证据边界。上述全部可在原新handler/受控/native文件闭合，不新增公共接口、旧helper变更或白名单。固定httpapi/problem.go/response_writer.go的实际stateOf依赖另以接受Update闭包指纹核对；未消费活动Model read修订。

其余完整结论：

- 六HTTP操作、strictpresence/null/key/UUIDv7/version Max-1、材料1..65536byte与400KiB raw预算、1KiB小body与安全response、closed DTO/HEAD一致schema均明确可实施；真实最坏转义编码而非算术、自有material Destroy及Go string无法可靠全擦除限制准确。
- Project lookup为明确新增契约，不冒已验。identity owners精确[Project,User]、scope/kind/ref/version/Model绑定；同Tx Command/User/Project SH→当前Session/Owner Read→安全receipt，scope端口分支与末端ctx/Unknown零观察，旧System接口不扩Purpose。writer阻塞不冒absent，lookup不证明材料同义或自动重发许可。
- Rotate/Delete复制的是原System目的保护控制流，不复制管理员权限。两次lookup上界+Metadata只是前置，任何历史命中仍唯一ExecuteWrite原请求；原Apply Mutate-before-receipt/摘要比对不变。archived被动Read成功与原mutation拒绝互补，Create Tx外stableRef发现/nonce预留亦准确保留，Secret没有Update的自动WithoutCancel3s确认。
- 同Store真实Secret checker→唯一ProjectAuthority SecretProducer map→Audit/SecretDelegate顺序可实现；无setter环/假checker/Store getter推断。保Account/System/ModelUsage实例与原catalog/Outbox，凭据没有新Project/Model事件；构造无I/O、原初始化与真实HTTP/root关闭边界明确。forced root后proxy/backend等待不倒称innerjoin。
- 22技术=9旧+13新、README另授。48snapshot逐hash同，13旧PG top均在匹配接受Update闭包的固定源中找到；普通/race/真实native/PG/原body/schema/坏事实委派/原writerACKloss/回滚/当前权限有明确归属和预算。Model read完整产品接受并正式移交后才能共同冻结根，当前活动实现未用于结论。

无Go、Node、测试、网络、资源或Git命令；只读固定源与自有scratch。保留此前模块原失败、opaque同Store构造限制及未绑定/ready503/三停止。待root协调作者新freeze后只差量核CRD-STATIC-01，不改作者卡或业务源。
