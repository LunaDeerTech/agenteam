# Project 配置与安全 chat 目录 Owner 只读 HTTP：正式 rev2 独立 STATIC

**完整限定 STATIC PASS，无剩余阻断。** 覆盖正式卡13技术路径及1个另授权README末件的完整规格，复用 rev1 已完成技术审查并独立核本次全部差量。不是产品实现、动态验收或资源授权。

正式卡：`docs/development/work-items/d09-project-model-owner-read-http.md`，SHA `97b70dfde863fa73b88c25dd14c6a7d95476282eee3ea91972b426c41b61cc36`。

- 8处变换已独立逆向还原为原 draft01 每个字节；除cursor修正外，只有标题、页首/末尾行政状态及4个正式来源链接。原13技术+1README范围、全部HTTP/Owner/DTO/8MiB/Unknown/I/O/root/验收契约保持。
- B1已闭合：明确无TTL/时间到期；当前加载keyring无kid拒绝，保留旧kid及对应key仍可验签，同时逐页重验当前权限与查询绑定。与已核固定cursor实现一致，不引入新公共行为或热轮换。
- 4个正式相对链接均存在、目标精确。D09配置库验收、Owner-read卡、模型配置设计指纹与rev1固定输入一致；Update正式卡SHA `b7596871feab7ed147fa59d4a3eb9b331a5c49ceb6296df2c60a1cd551b53b96` 与已接受header记录一致，和固定Update rev1仅行政接受页首不同，技术节至末尾逐字一致。
- Update必须完整产品接受并独占交接account.go后才可实施，依赖实际图后冻；Resolution/Invocations/D24与三停止等边界未漂移。没有读取Update活动18技术源。

复用原完整审查：`../project-model-owner-read-http-static01/review.md`（SHA `aeb1666372492fb84504c128b3223f2f890d8f4226c9935b198f7c8f9e5ea541`）。原rev1 NEEDS_REVISION与原输入证据保留，没有覆盖成PASS。

原报告列明的后继实现门槛保持：业务前Flush能力检查不能由提前Flush探测代替；预编码同一完整bytes后发布；8MiB预算先于新增Validate/clone/Marshal且Unknown优先；权限变化的目标Tx屏障与默认root/辅助fixture来源必须真实区分。它们均是后继实现/动态验收重点，不是本卡新增未决契约。

仅静态读源、SHA/差量/路径/格式核对及自有scratch写入；未执行Git、Go、测试、网络或资源。当前结论可供root正式采纳，实施须另授。
