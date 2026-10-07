# Partial7 / native03 STATIC 与独立 controlled 准备

结论：partial7 三个测试的限定 STATIC **PASS**；复用 production01 四源 PASS。追加 #5 native03 完整静态及 actualn 窄差量 **PASS**，没有 native 行为结论。独立 controlled 探针已完成必要离线准备，行为尚未运行。

固定输入：作者 unit-candidate01 manifest `0ae9e48c5d609e69609d76082302d75c3f77b3ff22fb1a8269583188e17090e9`、unit-input01 `86a59e0cd2f6b4367b2a6983b09f3913eea674149eeacfd5f2f320af949377d4`。后继 native03 manifest `6f382bad0174269542b87c4a7cada333b1f801c5be13b2fb1d004d94ebc90d8f`，native source `cfbfc01f8925ee52c06778dcd883c5ac0c0ad946598cae4ce19ea1c2bedf3006`，delta `127ce8ebcd1f1a3bcf0d9ffb5d879ec2eedc9f91c49c77d4f51fbfd9ab247ccc`。其余七源未变。

测试审查：两 model pure 共699行，app85行。typed invalid/严格 envelope/精确cap与cap+1 均核 Service计数；Project ref scope、MaxInt64 入原库、policy分层、error优先及私有投影错误均有直接断言。结果测试保正式 tracked writer code/已提交防二写。异步尾部通过接受 projectHTTPAsync 在launch前登记release/cancel/actualjoined，进入等待有界；失败前置断言仍能释放和实际等待。schema 使用实际encoder八样本、标准Draft202012/FormatChecker与本地refs，Python子进程10s context+CombinedOutput实际wait；不用schema样本证明真实权限。app原两个top逐字前缀保留，新增42个方法/资源组合和三种未所属lookup路径检查单次分派及原Request/URL/Body身份。

作者 unit-results01 的11项结果及各raw stdout/stderr指纹已逐项复核，均exit0、实际direct wait、双空和输入一致。普通/race各model4top137sub、app3top62sub，schema8/40负例与vet是作者证据，不列作本实例独立运行。没有重跑作者全集。

native03：自然30s写与2s lookup分别处于1m父ctx下，早父120ms保留；三case可装入40s测试预算，接受client每连接38s deadline不提前截30s案例。keepalive在lookup后等2.1s再写，用同TCP复核reset。WriteAndClose使用接受真实net.Timeout observer，新增asked/returned和阶段统计；HEAD要求write0/flush恰1且退出，POST lookup要求实际Write进入/退出和合法字节计数。`blocked_get_write` 是继承的mode名，该测试实际发POST，不宣称成功GET。4MiB安全测试header用于真实背压，不当作产品输入。监听器关闭、Serve实际终局、ConnState归零与非合作服务release/owners.Wait按当前调用链可清理；后续真实窗口仍必须核PID/TCP、实际net错误和终局。manifest的not_included遗留文案含native，但files/status/delta明确已冻8；本审以实际八文件及来源为准。

独立探针只有 `TestIndependentProjectConfigurationControlled` 一top，补有效候选仍保原error（七调用）、历史expected+1与被动lookup自身合法性、typed关联失败及嵌套转义重复键0call、reasoning policy保持原服务、HEAD拒绝零实体、Unwrap panic/64环界以及Close panic与阻塞取消callback重叠的actualjoin。复用已有惰性测试boundary/writer，不接Store或监听器。组合异步case在launch/首Fatal前登记幂等release+cancel+joined，释放后要求实际返回与零发布。

固定 partial7 overlay 将六Go源指向冻结snapshot、加入一私有源，并显式新native路径删除映射。原schema仍以固定hash绑定实际路径。目录门禁只排除此明确native文件名；不是忽略任意新源。integration目录不在实际编译图/fileset。实际normal/race图395/397包、2502/2508源，恰比作者图多私有源，无缺失/其他增加；两份生成test main与model/app自定义TestMain/init严格区分，后者均无。未变依赖init按接受图复用。

本实例已完成format、normal/race实际graph、normal/race model compile、两个精确list、race vet八项，全部exit0、45s外围内、subreaper实际wait、两次owned空、输入一致。list只出现该私有top，没有执行test body。固定driver继承已验owner-read driver的wait/监控逻辑，仅私有路径、overlay映射、明确目录排除和native禁用环境变化，diff保留；没有HOME覆盖。重启前无在飞执行，本次重新取得各命令现场PID；不继承旧资源基线。

`behavior-proposal01.json` 给出尚未获准执行的普通/race两条精确命令，各test40s/外围45s。任何失败先实际清尾、保原输入/raw再局部归因；没有native/PG/浏览器/真实root授权。完整14技术、README15和实际资源行为保持待验。全部写入仅本scratch；准备完成停写。
