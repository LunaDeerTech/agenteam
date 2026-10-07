# Partial7 独立 controlled 结果

**PASS，普通与 race 各一次。** 按 root 明确授权，执行固定 proposal01 `ca455af9dd8d0635ba57fe0997e97d258f3f4e0abbdb922254d5203805806868` 中两条命令，先普通完整收尾与输入门禁通过，再运行 race；无重试或输入修改。

- controlled-normal01：exit0，0.456s，1top/14sub节点。
- controlled-race01：exit0，1.419s，1top/14sub节点；无 race 告警。

14sub节点包含一个分组节点，实际13叶子；外层top另计。原始命令、固定输入、stdout/stderr 和 result 位于各同名目录，`controlled-results01.json` 绑定其精确指纹。两轮 direct child 实际wait，adopted wait集合为空、无残留/强制清理动作，owned连续两次为空、输入前后相同，stderr均为空。

独立补证包括：七服务的合法候选结果不能覆盖原error；历史version和lookup自身合法性；typed关联/嵌套转义重复键零服务；reasoning policy保持原库；HEAD拒绝无实体；Unwrap异常/64环界在认证前关闭原body；Close panic仍等待阻塞取消callback实际退出而零发布。controlled错误是受控分类证据，不冒真实COMMIT Unknown、当前权限或数据库锁证据。

复用 preparation-review.md 的 partial7/native03 STATIC 和八项离线准备；私有有效图仍明确排除本卡native及活动integration。没有新native、PG、socket或业务写入。完整14产品和README15保持待验。所有本次命令终局，无活动资源；本目录后继报告已停止写入。
