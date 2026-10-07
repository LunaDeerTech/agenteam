# Project configuration writes HTTP rev1：完整限定 STATIC

**结论：NEEDS_REVISION。唯一技术阻断 B1；另有措辞修正 D1。** 审查输入为冻结 scratch rev1，SHA 464b27f3f6244c60fab72f7b5613202e5453c7dfe6d5dff09895bb4f92a488cb。未参与建议/规格实施；只消费固定接受源及本次冻结稿，没有读取活动凭据实现、Go/产品测试/资源/Git/子委派。本结论不是动态 PASS，也未授产品实施。

## B1：强制真实旧 root 验收与新路由、白名单冲突

位置：rev1 §2/§7/§8.4；固定 additional-fixed/tests/model/project_configuration_http_root_test.go 第83行（SHA 3f2ee5b8e9a0f8d05c95976fa038240f43a822296289ef5d6a9dece4e759d12f）。旧 TestModelProjectConfigurationHTTPDefaultRoot 仍以 Owner POST /projects/{id}/models、nil body 断言405 MethodNotAllowed；新卡开放同一 POST，而§8.4明确要求这个旧top本次实际执行，§7又禁止修改旧fixture且未包含该文件。因此按rev1无法同时满足业务与验收，不能靠跳过旧top或复用先前read-only运行解决。

固定 project_usage_http_root_test.go:175–180 的 request helper 仅在body非nil时设置 Content-Type/CSRF/key。原nil请求按新认证顺序会先遭CSRF拒绝，不能只把405换400。最小修法：单独列旧root文件窄差量，在该单行将body换空JSON对象 map[string]any{}，利用原helper正式Cookie/CSRF/key，期待400 InvalidArgument；其余旧root断言逐字保持，仍实际运行整个top。不改共享helper，不以无凭据失败冒充到达配置输入层。

数量随之为14技术+README末件1，共15路径，10新/5旧。README原#14顺延#15；范围表、候选清单、输入/开工/交付计数同步。root已采纳该窄修方向；rev1仍保原件。

## D1：身份隔离措辞

§4“跨user/Project不能串行”应改为“跨User/Project不能串用identity/receipt”（或同义精确表述）。原 commandIdentity 为 model.project/[ProjectID,stable UserID]；隔离的是命令身份/历史结果，不禁止独立命令先后执行。这是文案精确性修正，无新技术规则。

## 已闭合的其他规格项

- **输入与policy分界：** 完整typed请求先验可拒坏Project/chat/credential-scope/nullable/target/capabilities，0服务调用；仅业务policy/当前版本/Secret purpose/同Tx权限留原库。固定httpProviderInput转换硬编码SystemScope，卡明确Project私有构造，不复用后修。合法reasoning=true+非空efforts可到原policy的CapabilityUnsupported，未改变通用Capabilities数组契约。MaxInt64输入合法，只有实际nextVersion或成功绑定阶段作安全溢出处理。
- **历史receipt：** 六方法Validate→Clone→runCommand；Project Read→原identity/完整semantic/receipt→首次Mutate，正式Tx重复Read/receipt/首次Mutate。更新/删除历史semantic含原expected，首次保存receipt就是expected+1；因此成功投影按原请求expected+1与已接受历史兼容，不依赖当前GET/资源仍存。Create=1；初次no-op拒绝；归档Read仍可历史重放。
- **两种Unknown与lookup：** 原库Unknown在原ctx、原Command EX下私有确认；无新增WithoutCancel/自动lookup，原error优先及cause/attempt保真。公开lookup仍found/receipt，Command EX+User/Project/model-references SH，合法absence不是回滚。新写后坏receipt DependencyUnavailable/Unknown是明确新私有HTTP分类，可能已经写入；无伪造cause/attempt，不能冒物理Unknown证据。lookup坏union NotStarted只表示本次观察不提供，不判原写；现Fault/Problem可安全表达两类状态。
- **边界与表示：** 1MiB原DecodeJSON具完整扫描/duplicate/UTF8/depth/字段和原bytes解码；当前typed字段、原policy与1MiB界不互相代替。闭合ASCII安全receipt的保守结构上界155 B、lookup外壳180 B，故1KiB工程界可实施；这里只做静态编码算术，不冒Go实际最长例测试。旧五读8MiB与动态配置对象保持。
- **权限/数据：** Project凭据Ref取path scope、Purpose由原Secret计划验证；缺Agent canonical rewrite时Project任意reference拒绝、无引用replacement只沿原同Project/启用System chat规则。Secret引用、Audit、Outbox、receipt原一笔Tx；不可index-only改写，无管理员跨Owner豁免。权限/归档/引用辅助SQL事实须明示不等于生命周期或账号创建E2E。
- **I/O/root：** 私有能力预检与tracked writer透明链、逐setter/Close panic收尾、实际callback join、正常publish后reset、error/HEAD同尾部均明确；原ctx耗尽不发迟到成功或Problem。发布预算不是Store/Close整个硬返回界。可仅app/project_models.go组合已构造同一Model/boundary，精确资源/方法及完整Allow，保现五读和其它root路由；凭据完整产品接受/实际根移交是开工前提，不从旧account snapshot覆盖。
- **可执行验收：** pure与native分离；native唯一30s慢body+2slookup/早parent在40s计划内，真实小response Write/Flush的实际调用、字节/timeout和受控等待限制须后继实证，不能伪errno。五PGtop120s含Cleanup/包6m、原7资源、fresh/live新基线、actualwait/doubleclear、失败停后继均明列。图需按凭据最终接受输入重冻，接受图20个生成test main不等于自定义TestMain/init无资源；执行前按实际选包核这一点。原代理须同连接目标COMMIT/Z(I)/drop，禁止把socketclose或私有坏receipt分类代替真实事务证据。

## 输入核查、复用和后继门槛

input-check.json（SHA 7499e1c6d7f5d7637d185c93c0319a73894ce5a61be7d9e9b04f7ca8014b5a60）记录：45个固定源逐hash一致；10个文档链接存在且固定；14候选路径唯一/10新4旧；19个既有验收top可在固定源定位，另5个凭据top仅对应已接受规格，未当产品已通过。另两份公共Fault/Problem源先与接受read实际graph指纹匹配，才读并复制到自有fixed小目录。无活动源码归入证据。

复用已完成PREPARED和接受Model/read事实；原规格/产品历史失败不覆盖。本次没有运行Go、测试体、schema产品验证、socket、PG或任何资源，不复用旧资源基线。建议一次rev2仅闭合B1/D1及对应行政计数；冻结后差量审，并保rev1 NEEDS_REVISION。凭据产品仍未接受、本卡尚未正式归位、独立产品/资源验收及README均未发生。报告完成即停写，无活动命令/资源。
