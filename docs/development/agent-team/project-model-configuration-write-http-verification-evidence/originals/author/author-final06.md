# D09 Project Model configuration write HTTP 作者限定验收（candidate06）

状态：作者版本组合限定 PASS，提交独立完整验收；尚非产品接受。当前 14 技术文件逐 SHA 与 candidate06 有效继承快照一致，生产四源自 production01 未变。README #15 未授权、未修改。作者全部实际窗口已归还，无在飞命令或 owned 资源。

- [总索引](author-final06.json)、[最终 14 源](effective14.json)、[candidate06](candidate06/manifest.json)。
- [离线原结果](offline-results02.json)、[最初纯测试](unit-results01.json)、[最后受影响检查](candidate06/offline-result.json)。最初 Model 普通/race各4 top/137 sub，app普通/race各3 top/62 sub；实际 encoder schema8样本/40负例。最终受影响 racecompile、精确13名list、vet均actual0/actualwait/owned双空/inputsame。既有普通/race/两cmd/实际图按未变源复用，不重跑已经通过且未受影响项。
- [最终实际图继承](actual-graph-full05.json)、[最终离线输入](full-input06.json)、[PG闭包](pg-driver-v01/closure05.json)；复用接受凭据的完整实际 Go/import/embed/TestMain20包/两动态cmd/CGO0server差量，没有复制依赖树或改依赖版本。

## Native 原件与版本组合

[四轮索引](native-results02.json)：v01 keepalive 1 top/0 sub，3.153430s；v01 slowbody 1 top/3 sub，33.202296s（自然写30s/lookup2s/较早父期限）；v01 writeclose首次FAIL，6.940950s；v02 writeclose复测1 top/10 sub PASS，7.951028s。每轮均实际direct/adopted wait，owned PID及所有TCP含TIME_WAIT双空，输入不变；40s测试/45s执行与15sowned尾部/75sTCP退役分别记录。

首红仅HEAD旧合并判据把读取错误或字符串application/problem+json混为一项。原raw未区分分支，不能倒填。candidate03只修私有测试观察：唯一WriteHeader405、Write0、Flush进入/结束1、实际net timeout；复测ReadAll正常EOF，收到8192字节合法部分头，header_complete=false。该轮不冒充实际完整头后body检查触发。旧label blocked_get_write实际执行POST lookup，不是新GET成功。生产预算、旧共享helper、安全closed trace均未变。

## PG 九轮与最终覆盖

[九轮原件索引](pg-results06.json)保存3 FAIL、6 PASS，最后5新+旧8共13 top/35 sub的版本组合限定PASS；每轮完整四容器/三网络、actualwait、七资源与owned双清、watchdog线程join、前后输入同，forced/adopted均0。每新top120s含Cleanup，原包6m，每轮fresh至少5GiB及新live PID/starttime/Docker基线，任务空Docker config仅inspect两固定本地digest，无pull。

| 实轮 | 结果 | 保留的范围 |
| --- | --- | --- |
| new-crud01 | FAIL | 首次私有snapshot SQL引用不存在models.project_id；generic DATABASE_SQL_FAILED未保存具体PG细分 |
| new-crud02 | PASS | 6写、原receipt/replay、完整输入拒绝、版本上界、ref交换/释放、五旧读 |
| new-authority01 | FAIL | 正式Login/Logout后仍比较登录前全refs快照；原raw未标实际差异bucket |
| new-authority02 | FAIL | 两处辅助archiving SQL缺operation；其余已通过sub不能充整topPASS |
| new-authority03 | PASS | 1 top/11 sub，正式权限、同Txgate真实Forbidden完整rollback、EX poison、未绑定adapter、生命周期历史 |
| new-unknown01 | PASS | 1 top/4 sub，原Store Unknown、原ctx确认、公有EX lookup分列 |
| new-root01 | PASS | 1 top，默认app.Run init失败无监听、原Authority六写/旧口、重启稳定 |
| new-shutdown01 | PASS | 1 top/4 sub，3s/100ms writer与slow body |
| old01 | PASS | 原8 top：Summary、Model read、Credential root/CRUD、OwnerUpdate、Usage、System CRUD/atomic |

三次PG修法均仅本卡测试：candidate04 #9一行models→provider JOIN；candidate05 #11在合法Account setup前后精确比较5 Model bucket及consumer=model完整引用行，再以新完整snapshot检401与后续拒写；candidate06 #11复用既有正式BeginArchive gate，preparation使用独立Project/本scope credential，结束只还原fixture project/scope，避免操作污染主Project。Archived沿既有helper提供显式终态Authority输入，不宣称participant runtime stop。原snapshot全refs保留；Model.references索引精确覆盖由独立A补，作者不混称Secretrefs已代表该索引。

首红与修因：[native](candidate03/first-failure.json)、[snapshot SQL](snapshot-query-plan01/finding.json)、[Account setup](candidate05/first-failure.json)、[archiving setup](archiving-setup-plan01/finding.json)。所有旧候选和delta保留。

## 安全 body 与终态边界

[31份原bytes/旁car索引](body-index06.json)：本卡18份（CRUD8、Unknown4、defaultroot6），旧回归13份。各原件绑定实际run/input/status/Content-Type/X-Request-ID/target及hash，未重编码，未保存key/CSRF/cookie/配置请求或凭据材料。失败轮不倒填为成功来源。

作者heldwriter先关闭client触发原Store Unknown，之后实际COMMIT或ROLLBACK C+Z并丢弃ACK；不能称独立terminal-first。原Unknown cause/attempt、同原ctx私有确认及公有Command EX lookup分别由原测试断言支撑。lookup-read-unknown仅为真实read后受控terminal替换，不属于物理wire ACKdrop。默认root强制返回也不能独自证明每个内部owner已join，实际client join及原Command EX终态是另外的已验事实。

## 资源与接受边界

本批PG live基线PID1 shim Z为284，九轮各新增4，最终320；新增36均非作者owned、未wait。每轮历史git/go/compile等按原result分列，没有全机零僵尸结论。作者owned链actualwait/七资源双清不覆盖Docker daemon父链。

root曾只读展示metadata误取owned_empty而非实际ownedempty，修正原JSON读取成功；一次root只读exec短暂transport错误随后pwd成功。我方shutdown session实际返回0/wait/双清，未以连接错误推断进程退出。这两项仅协调工具事件，非产品失败。

完整独立技术接受和README末件仍待root后续。未接受完整D09/D24，生产Resolution/Invocations与三个stop依赖边界未扩大。
