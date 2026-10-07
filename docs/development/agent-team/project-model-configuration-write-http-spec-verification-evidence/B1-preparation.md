# B1 窄修准备（非实施）

仅 scratch 记录；rev1 与其 freeze 保持原字节，等待完整独立反馈后一次 rev2。

原接受 root 测试 `project_configuration_http_root_test.go:83` 将 Owner POST models 断言为405。其固定 helper `project_usage_http_root_test.go:158–180` 只在body非nil时设置Content-Type、X-CSRF-Token、Idempotency-Key；nil原样改期望400会先在正式CSRF得到403，不能证明新写输入校验。

root已选择：只把该调用body换 `map[string]any{}`（真实编码 `{}`，helper按既有行为附正式CSRF/key）并断言400 `InvalidArgument`；不改共享helper、不删除旧top或断言、不改其他read/Update历史/ready/init/drain/restart检查。`B1-proposed-product-test.patch` 是将来实施的单行语义示例，不曾应用工作树。

rev2拟新增技术#14 `tests/model/project_configuration_http_root_test.go`，仅上述POST调用适配；原README顺延#15，其余#1–13不变。总14技术+README15，10新/5旧。§8.4继续要求该旧top本卡真实执行；§7旧fixture禁止写的总句为此唯一路径/单调用给显式例外。其余额外修改不得推定授权。
