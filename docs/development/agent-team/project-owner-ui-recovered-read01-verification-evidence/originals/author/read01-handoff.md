# New implementation read01: actual failure, retired, STOP

The sole root-authorized round executed the full unchanged fixture chain with new-read only. Authorization SHA `af1c94db9b1ab1a4155b1dbc150c9f592b573013c4fd1062682883137091fb72`, driver `bb666f55bf39f6454a370f62459d3e72416d90767f14a75d2e99775b8c11b38e`, Go candidate04, browser-v2 `9ca0cefb52b4c67d1d5f84600c6bdecb6886fda0927898cff0ca24365cb3ccae`. Source/product/driver/JS/dist were not modified by this author. No successor or retry ran.

Run originals: `/workspace/scratch/owner-ui-backend/resource-driver-v01/runs/read01`. `read01-handoff.json` binds all93 retained run files and the launch log. Raw SHA `c5cf12b7a1cf93e4d060d03eff89c89966c4ae3e04ae98006884f544029f0e8d`; result SHA `d1b1377b225ba356b11869f01e4641e8eb8fc38ab1578dc0feb60e6f6d99c05b`. These are new implementation originals, not reconstructed historical read01/read02 artifacts.

## Failure and evidence limit

`TestAccountProjectOwnerWebReadAndNavigation` failed in17.21s. Browser-v2 `project-owner.spec.ts:465` waited5000ms for `getByRole('heading', {name:'项目不可用', exact:true})` after navigating to `/<owner>/owner-deleting/settings/general`; the locator was not found. The Node test exited1, the account package failed, the fixed full package chain finished, and the driver plus outer exec session88749 actually exited1. Driver command duration114.299s includes the original builder/full-package chain and owned process retirement; TCP tail observation is separate.

The last preserved safe sidecar `response-035.json` is actual GET `/api/v1/projects/resolve`,409, `application/problem+json`. Its matching raw body SHA `5c481df4498f0f277a63120b04d4620e464a13cdea76f479af74adab6239dfe6` contains `code:PROJECT_NOT_ACTIVE`, `title:Project not active`, `detail:The project is not active.`, `commit_state:not_committed`. The frozen controller's `denied()` recognizes only401/403/404; its read-failure path therefore classifies this409 as read-error, and the frozen view maps read-error to“项目信息读取失败” while unavailable maps to“项目不可用”. This source/response correlation explains the mismatching expectation; no retained DOM snapshot independently proves the final rendered heading. Root's independent review determines whether the contract requires a product mapping correction or an expectation correction.

The dotted direct-navigation statement is immediately after this failed assertion and was **not executed**. No dotted real-browser PASS is claimed. The round retains35 safe sidecars and34 unique raw bodies, all body hashes match their sidecars. There are0 screenshots and no `schema-validation.json`; the incomplete round does not establish the final same-body schema/client acceptance or a complete read-top result. Safe evidence remains under the top's `safe-http-evidence` directory; private credentials/runtime material was cleaned, not copied into this manifest.

## Actual retirement scope

Fresh free disk was24.3GB at both preflight and immediately before spawn, above5GiB. The driver recorded live PID/starttime, Docker and two all-state TCP baselines, used the private empty Docker config, and checked both exact local PG digests without pull. The observed topology was7 exact IDs: four containers and three networks with the three fixture nonce-label families.

The Go log records actual Node direct wait, proxy Serve/body-handler join, preparation Project service join before ProcessGuard release, and default-root join. The outer driver completed the direct wait,4 adopted-child actual waits, watchdog actual thread join, and0 forced tail actions. There were0 monitor errors or cancellation requests. Both cleanup samples show all7 exact resource IDs absent, unrelated Docker baseline unchanged, no newly remaining resources, no owned processes and empty general/browser private runtime directories. Source/tool/fileset/dist and frozen verification inputs matched before/after the resource round.

The supplementary TCP/tcp6 host delta, including TIME_WAIT, became empty twice after7.235s tail observation. This polling neither assigns connection ownership nor proves every brief connection; no PID was signaled from TCP inference.

Four new PID1 `containerd-shim` zombies are separately recorded:83411/start516592,84098/start517308,84466/start517522,85086/start517791. They are daemon-owned, not this driver's children, and were not signaled or represented as joined. Owned double-clean is not an assertion of global machine emptiness. Historical PID1 zombies were left untouched.

Root subsequently restored the original three-file `web/dist` only after actual readers retired, preserving the53-file test copy under `/workspace/scratch/owner-ui-assets/retired-read01-dist`. That later root operation does not rewrite this round's input-before/input-after evidence. This author has no remaining command, reader or owned resource and now stops for root's review; no fix or rerun is authorized by this handoff.
