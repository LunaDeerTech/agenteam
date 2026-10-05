# Invocation / Usage ledger rev1 规格自查

候选规格，尚未独立审查或采纳；无业务实施权。固定业务 4295df7d51c1f171df78ab3f0d9cef2fd241a505。

实际核对 C0 Reader/Invocation/Summary、ConsumerAuthority/Attempt/Input、Secret planned read、Exchange Observe/Joined、Project AuthorizeProject、Store/Tx/locks/cause、cursor Keyring。保持旧 C0 两个 Reader 签名和 nullable/count 约束；新 InvocationFacts/Writer/opaque plan 是本卡明确新增口，不冒充已存在API。

20候选路径=9生产+1迁移+10测试；仅既有 identity.go/identity_test.go 窄扩角色闭集，原 Model/Secret/Project/adapter/app/C0 carrier/旧SQL不改。00018仅唯一规划保留。三表分别为 canonical latest Invocation、完整逐观察幂等回执、可重建Execution统计投影；没有calls或调用许可占位。

17相对链接/fragment按固定Git检查；UTF-8/LF/末换行/尾空格/fences及新文件限定diff检查通过。private inputs仅19个Git blob定位，不复制源树。

仅新卡和本私有目录写入；未运行Go/SQL/Docker/npm/browser/网络，未恢复Object暂停任务，无业务/Git写、无资源或后台命令。独立规格审查待主线程调度。
