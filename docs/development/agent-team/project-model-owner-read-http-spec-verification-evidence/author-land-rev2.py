from pathlib import Path
import difflib
import hashlib
import json
import re

ROOT = Path('/workspace/scratch/project-model-owner-read-http-spec')
REPO = Path('/workspace/agenteam')
SOURCE = ROOT / 'draft01.md'
TARGET = REPO / 'docs/development/work-items/d09-project-model-owner-read-http.md'
OUT = ROOT / 'rev2'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


assert not TARGET.exists(), 'new card already exists; stop without overwriting'
assert sha(SOURCE) == '9c0062bee024794f85b00f3581aa3782e4f0040ada3110989313c591b563fafe'
assert sha(ROOT / 'inputs02.json') == '0b3ba4165bb0ef2f951a4883281221626d2a9c4bf3d6c02f831ab9e74e6af311'
assert sha(ROOT / 'cursor-clarification01/proposed.patch') == '80f56ce48a1eba08056298f2088d561a6fd95eb55f0744cd7fc46991c3acbbc0'
original = SOURCE.read_text()
intro = original.split('\n\n')[1]
final = original.split('\n\n')[-1].rstrip('\n')
replacements = [
    ('administrative_title',
     '# D09 Project 配置与安全可用目录 Owner 只读 HTTP（scratch 工程草稿 rev1）',
     '# D09 Project 配置与安全可用目录 Owner 只读 HTTP'),
    ('administrative_status_and_source', intro,
     '修订：rev2，2026-10-07，**待独立差量及正式卡完整 STATIC 采纳，未实施**。本轮唯一仓库写入为本规格；下列实施白名单不是产品、Go 检查或资源授权。固定产品输入为 root 已接受的 `901eb54605d293d4308caadd278c2c3a7ae1b824`；D08 Update 仅消费已接受规格 `03d1c107`，其活动实现尚未接受，完整产品接受与共享根交接门槛保持。固定来源索引 `/workspace/scratch/project-model-owner-read-http-spec/inputs02.json` SHA-256 `0b3ba4165bb0ef2f951a4883281221626d2a9c4bf3d6c02f831ab9e74e6af311` 原样保留；原 scratch draft01 与 cursor 问题记录不改。root 已采纳下文 8 MiB 完整表示限制及输出无法提供时的原 503 语义。本修订仅归位标题、状态与正式文档链接，并纠正 cursor 无时间到期语义；其余技术内容保持 draft01。'),
    ('formal_link_d09_acceptance',
     '](/workspace/agenteam/docs/development/agent-team/d09-project-configuration-verification.md)',
     '](../agent-team/d09-project-configuration-verification.md)'),
    ('formal_link_owner_read',
     '](/workspace/agenteam/docs/development/work-items/d08-project-owner-read-http.md)',
     '](d08-project-owner-read-http.md)'),
    ('formal_link_update_accepted_spec',
     '](/workspace/scratch/project-owner-update-http-spec/rev1.md)',
     '](d08-project-owner-update-http.md)'),
    ('formal_link_model_rules',
     '](/workspace/agenteam/docs/architecture/platform-infrastructure/model-system/model-configuration.md)',
     '](../../architecture/platform-infrastructure/model-system/model-configuration.md)'),
    ('cursor_only_technical_correction',
     '更换同一 User 的有效 Session/合法变更 limit 可以续页；跨 User/Project/三查询种类、System cursor、篡改/过期 token 都拒绝。',
     '更换同一 User 的有效 Session/合法变更 limit 可以续页；跨 User/Project/三查询种类、System cursor、篡改 token，或签名 kid 已不在当前加载 keyring 中的 token 均拒绝。cursor 无 TTL/时间到期语义；轮换后保留旧 kid 及对应 key 时，原 token 仍可验签，且必须继续通过每页当前权限和查询绑定检查。'),
    ('administrative_current_delivery', final,
     '当前产物为本正式规格 rev2 及原样保留的 scratch 输入/差量记录，待独立差量与正式卡完整 STATIC 采纳。自查只核文档来源/路径、JSON、哈希、结构与格式，不运行业务/资源；自查不叫独立 STATIC。后续实施与资源仍须 root 另行授权，并满足 Update 完整产品接受及共享文件交接门槛，不因本卡落盘自动启动。若后续实际图、Update交接或正式库发现新公共缺口，仅暂停相应部分并给具体源/失败证据，不能在新HTTP里造替代端口。'),
]
text = original
for kind, before, after in replacements:
    assert text.count(before) == 1, (kind, 'must replace exactly once')
    text = text.replace(before, after, 1)
restored = text
for kind, before, after in reversed(replacements):
    assert restored.count(after) == 1, (kind, 'reverse mapping ambiguous')
    restored = restored.replace(after, before, 1)
assert restored == original, 'unclassified content change'
assert text.endswith('\n') and '\r' not in text
assert not any(line.rstrip() != line for line in text.splitlines())
links = []
for label, link in re.findall(r'\[([^\]]+)\]\(([^)]+)\)', text):
    assert not link.startswith('/'), ('absolute document link', link)
    resolved = (TARGET.parent / link).resolve()
    assert resolved.is_file(), ('missing formal link', link)
    links.append({'target': link, 'resolved': str(resolved), 'sha256': sha(resolved)})
assert len(links) == 4
OUT.mkdir(exist_ok=False)
with TARGET.open('x', encoding='utf-8', newline='\n') as handle:
    handle.write(text)
patch = ''.join(difflib.unified_diff(original.splitlines(True), text.splitlines(True),
    fromfile='scratch/draft01.md', tofile='docs/development/work-items/d09-project-model-owner-read-http.md'))
(OUT / 'draft01-to-formal-rev2.patch').write_text(patch)
result = {
    'status': 'AUTHOR_DOCUMENT_SELF_CHECK_PASS_AWAITING_INDEPENDENT_STATIC',
    'card_path': str(TARGET), 'card_sha256': sha(TARGET),
    'draft01_path': str(SOURCE), 'draft01_sha256_unchanged': sha(SOURCE),
    'inputs02_sha256_unchanged': sha(ROOT / 'inputs02.json'),
    'cursor_finding_sha256_unchanged': sha(ROOT / 'cursor-clarification01/proposed.patch'),
    'patch_sha256': sha(OUT / 'draft01-to-formal-rev2.patch'),
    'changes': [{'kind': kind, 'before': before, 'after': after} for kind, before, after in replacements],
    'only_technical_change': 'cursor TTL/retained-versus-retired-kid precision',
    'all_other_bytes_reverse_match_draft01': restored == original,
    'relative_document_links': links,
    'UTF8_LF_final_newline_no_trailing_whitespace': True,
    'new_card_previously_absent': True,
    'candidate_scope': {'technical': 13, 'README_last': 1},
    'repository_writes': [str(TARGET)],
    'git_go_business_or_resources': False,
}
(OUT / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({k: result[k] for k in ['status', 'card_sha256', 'patch_sha256',
    'draft01_sha256_unchanged', 'inputs02_sha256_unchanged', 'all_other_bytes_reverse_match_draft01']}, ensure_ascii=False, indent=2))
