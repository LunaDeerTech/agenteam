# Project Model Owner read HTTP — independent controlled offline

限定 PASS。固定 production03 + offline-candidate03（9源），复用原五源 STATIC PASS；新增测试清理差量 STATIC 闭合。独立 private overlay 只新增一个 model 包 test，无产品写入。

actual normal/race graph 为 395/397 包、2491/2497 源，相比作者有效图仅增加该私有源，无移除。普通/race TestMain 实际输出保存在 graph-delta01；候选 integration4 不在本次编译/执行依赖。图之后只改私有测试函数体，imports、虚拟路径、top 名及所有非私有输入不变。普通/race 编译通过；精确 list02 一项结果沿用到函数体修订03。race vet 通过。

单 top / 8 sub 普通与 race 全通过：262144 个合法 32-byte efforts，最小 JSON 9,175,041 bytes，完整夹具 Validate 在测量前成功；HTTP 编码拒绝前增量 TotalAlloc 两次均168 bytes。该证据只覆盖本代表的增量 HTTP admission，不担保数据库/前序库分配、RSS或所有 Go运行背景分配。typed Read Unknown 对同一大候选的 GET/HEAD 保留原错误指针、cause、attempt；预算拒绝 GET/HEAD 为503且零成功投影，HEAD零body；缺少唯一 Flush 能力时下游 Check/Auth/Service 调用均0；完整ASCII/Unicode JSON escaping与RawMessage保守边界通过。

标准 Draft202012 schema：3个生产encoder原输出正例、9个泄漏字段/不完整配置/错误scope负例通过。这是受控 encoder 字节联验，不是真实 HTTP/PG。

保留 compile01 原始红：私有 typed ID 不提供 IsZero，改用零值比较；执行前同步修正私有URL完整Project前缀。compile02通过后，执行前静态补齐夹具 Reasoning=true，受影响普通/race重编03。未修改任何产品以迁就测试。

每条命令45s上限，subreaper实际 direct wait、双次owned空、输入一致；无native/socket/PostgreSQL/全app运行。runner 原 fingerprints/processes/descendants 函数AST与作者原件完全一致，控制/终止/wait主循环原样；仅输出目录及复用图/工具输入展开有变。本轮没有产品剩余阻断；整卡13路径、native/PG及真实currentOwner仍未验收。
