# 当前执行检查点

- 目标：普通 Project Variables Owner 管理界面；已有 initialized Project 当前 Human Owner 经正式六 HTTP 完成列表/分页/创建/编辑/删除及原 intent 查询/显式重放。
- 状态：进行中，SPEC rev1已获独立窄审接受；API/client首片段已可构建并有定向纯控，Session首片段已可构建；controller/页面与动态验收尚未完成。正式来源：[工作项](../docs/development/work-items/d27-project-variables-owner-ui.md)。
- 分支：`ai/project-variables-owner-ui`；工作树 `/workspace/agenteam-project-variables-ui`；正式基线 main `3cea6076bb01693ead2755826826d189626aa3aa`。此前普通变量后端已交付，不重做其库/HTTP/迁移。
- 独占新增 Variables api/composable/views/tests及本树两文档；root已授client/useSession/router(auth/index)/App/ProjectSettingsView/frontendREADME七共享路径的本域最小增量。只读复用Owner currentReadContext，唯一Cookie owner/私有CSRF不变，整合保留WorkUI和Model相邻增量。
- 不含Secret、Agent、环境注入、Project创建/完整生命周期或生产SPA，不解除停止项。全局产品事实见[台账](../docs/development/agent-team/tasks.md)，不复制其他树流水。
- 已恢复两套本树私有node_modules，package及lock与同源逐字相同，无下载；CoW探测不支持，首次容量门槛拒绝后经root授权只回收旧关闭Variables作者gocache再普通复制。最终当时余量6.90GB。
- 首片段：web/src/api/project-variables.ts、web/src/api/client.ts、web/src/tests/project-variables-client.spec.ts；strictTS60055/30168均actual0，新client及旧Owner客户端102纯控47593 actual0。日志在output/ai/project-variables-ui/implementation/api-{types-01,types-02,pure-01}.log。原大patch语法/测试写入路径setup失败未落产品片段，已纠正，不冒产品红绿。
- Session片段：useSession.ts及project-variables-session.spec.ts闭合；32新控70294实际0、strictTS84960实际0，旧authentication61在首组合实际通过。首两轮15失败为Problem fixture缺X-Request-ID（第一次替换未命中，原FAIL保留）；新历史receipt变化反例24090实际红后最小固定校验，修后32控通过。逻辑超时仍等待原cancel尾，confirmed历史不倒退/不换receipt。
- 下一步：继续Session→controller/view→真实harness。未build dist、未运行PG/socket/browser；schema实体验证与真实产品验收未完成。
- Git及真实资源归root；源在本树正式路径，输出 `output/ai/project-variables-ui/`。冻结后等保存ACK再重开，不把checkpoint称为验收。
