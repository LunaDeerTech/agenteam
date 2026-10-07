plan02 限定 STATIC PASS，E1 已闭合。只修改原超限断言块，块外全部字节等于 plan01，构造、透传、写入、收尾、普通路径及原 Unknown/cause/attempt 不变。

在 later GET 前定位唯一实际 drop 连接；该连接的完整大 D 长度须在 (1 MiB,16 MiB]，随后按序出现 send-COMMIT、tag-COMMIT、ready-I、drop，缺失、跨连接或顺序错误均不能完成最终阶段。stage 索引有界。

只通过冻结方案静审，未运行 Go 或资源，尚非实际新 Unknown 证据。原失败和真实后继门槛保留。
