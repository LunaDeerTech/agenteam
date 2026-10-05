# 简短结论勘误

`preliminary-summary-original.md` 保留最初交给主线程的临时简报原字节，SHA256 `24e83a760d7b65feca42a3864879467c3dd7669b15ce741e2cec8ed593d893ba`，不作为最终准确叙述替代正式报告。

其中“下一轮在 Go 编译阶段因 /tmp 容量耗尽退出，未进入 probe”不准确。全文核对 `logs/independent-real-2.log:65–88` 后确认：多包编译报 no-space，但 Go 并行执行仍启动已构建的 objects test binary；2 个顶层的全部 6 个子例均在 `newTransferFixture` 初始化返回 `OBJECT_PAYLOAD_MISSING`，没有进入目标 stop/GET 业务及其断言。实际该包 FAIL 5.485s，整轮 driver exit 1，原日志、输入及二次资源清零记录完整保留。

同轮磁盘错误与初始化红并存，但未独立证明二者唯一因果。正式结论仅是：只迁移本人 cache/TMPDIR/可重建输出后，在全部相同 13 源、相同 probe、相同断言和预算上重新执行全部 2 顶层/6 子例通过。不得将原轮写成“所有测试未执行”“初始化原因已证”或“产品故障已修复”。正式报告已作此纠正，最终窄卡通过与资源清零结论不变。
