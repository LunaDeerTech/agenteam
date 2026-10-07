# HTTP 独立离线结论

candidate06 有界独立 HTTP offline PASS，实际 race 单 top、11 个嵌套节点（9 leaf）通过。图 376 packages /2370 files extra0，selected package 无 init/TestMain；race compile、vet 与精确 list 均通过。六条命令各 ≤45s，actual direct wait、subreaper、输入前后一致与两次 owned-empty；没有 socket、数据库、容器或原自然30s组执行。

独立代表验证缺 Flush 的直接/Unwrap writer 在调用前 abort；PATCH 64KiB 与 lookup 1KiB 的精确合法编码和多一字节拒绝；原 Body.Close 与 cancel callback 两个尾部分别阻塞，先释放 Close 仍不可返回，释放 callback 后才实际退出，零迟到发布、原 Close/服务各一次、deadline 清除。

U04 在 candidate04 的前置遗漏与 candidate05 的 preflight panic 清理孔均保留静态原件。candidate06 先建立安全 adapter/finish defer 再原位有界解析，逐能力保留首接收者及 FlushError 优先级，不提前 Flush；STATIC 修复闭合。作者人工 cycle 首红位于旧 RequestID middleware 的 stateOf，不能宣称本 handler 的有界解析修好了整个旧 middleware；06 相应 controlled 用例在正式 RequestID 建立后注入人工坏 writer。

本 worker 的首 graph 调用误传 plan SHA，输入门禁在 Go 子进程创建前拒绝；graph-gate-first-red.json 保留该工具调用事实，非产品首红。修正精确调用 SHA 后才开始实际 graph/compile/run。原未格式化草稿和格式差量保留。

真实资源、实际 Unknown/root shutdown、标准 schema 对同一真实响应字节的验证尚未由本 worker 执行，继续独立 A/B 准备；不能以本结果代替完整产品验收。
