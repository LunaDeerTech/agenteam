# Audit query01 单源独立 STATIC

结论：**限定 STATIC PASS，未发现阻断**。仅审正式卡 #1 新 `internal/central/audit/project_query.go`，不代表测试通过或完整14路径接受。其余13活动源没有读取。

冻结 manifest SHA256 `7b86715dd24bc2d7410cac4dde0e10ca9c0acc6eeb0ef346d5e20f7df69d797a`；204行候选 SHA256 `20ef9c44e53d6a50d515bfb2485981194cddc979e883fc75229922fc31c23456`，基线 `cc850b2244cad771eb862a2c82d99887d5da7284`。13项冻结依赖与已接受快照逐hash相同，旧查询／System／Store／contract未改。

已完整静读的关键路径：

- 行35–67：List/Get候选只在共同projectRead成功后返回，任何错误都返回零结构。Get复用原隔离SQL与scanRecord，再核精确scope和请求AuditID；列表采用非nil strict check，未绕回Tx外旧List/Get。
- 行70–120：nil parent与ProjectID、Human actor、nil/typed-nil端口检查；3s继承更早parent；只有一次WithinTx，原RecoveryCause owner固定audit.project-read。依序User SH→Project SH，在同callback中authorizeHuman（真实当前Session、Project Read grant binding）后才InTx与查询；新SQL executor也拒nil。未新增Command EX、权限预授权或额外确认事务。
- 行122–142：NotCommitted原Fault，Committed须真实返回且callback完整、当前deadline未过；否则零候选。Unknown Fault为CommitUnknown/Unknown、lookup hint，原有效attempt作内部CauseID；WithCause保存整个原CommitResult。公开ProjectReadUnknownAttempt以errors.As取该私有值，保留原Cause/AttemptID，不用新request/run替代。旧System行为不变。
- 行146–182：filter/page和cursor验证位于当前授权后；旧canonical filter digest、Project scope、AuditOrder与两scalar shape不变，limit仍不入digest。首返回行也必须严格小于cursor位置；后续CreatedAt／canonical UUID顺序严格递减，重复和逆序拒绝。每行包括哨兵都检查scope及完整filter绑定。
- 行187–204：时间下界包含／上界排除，ActorKind/ActorID/Action/Outcome/ResourceKind/ResourceID/Tool/Execution/Operation/Approval/Runner全部对应旧SQL；AgentID保原AgentRun actor或Agent resource的OR语义。未把53合法filter收窄为31输出。
- 旧query.go扫描先重建typed Entry和receipt，strict recordPage扫描limit+1并拒更多行，Close后Err先于发布／签cursor。新check确实进入此strict分支，因此坏第201／额外202与尾部Err不会被前页成功掩盖；仍需后续受控及真实测试证明具体执行。

作者唯一已核运行是gofmt：`query-format02/result.json` SHA `33245d88155093e4b9e4e2d7181ad34baad3527e919edd618846c6ae3efbf45c`，实际exit0/direct wait、两次owned空与输入相同。没有compile/unit/race/vet/native/PG结论；本独审也没有运行Go或资源。无生产改动建议，后续依卡补取消／callback终局、当前权限与锁、cursor/sentinel、物理Unknown原CauseAttempt等动态证据，并在HTTP/root完整冻结后核wire和实际尾部。

仅自有scratch审查记录，未修改产品／正式文档／Git，无再委派。输入末核保持原hash，已停止写入。
