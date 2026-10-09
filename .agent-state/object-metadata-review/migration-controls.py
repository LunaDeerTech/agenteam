#!/usr/bin/env python3
"""Reuse the independently owned pure mapping oracle for the one new literal."""
from pathlib import Path

source = Path(__file__).with_name('mapping-controls.py')
text = source.read_text()
changes = {
    "SELECTOR='^TestObjectMetadataCleanup(BoundedHistoryAndFinalTransaction|FinalCommitUnknown)$'":
        "SELECTOR='^TestObjectMetadataCleanupIndexMigration$'",
    "NAMES=['TestObjectMetadataCleanupBoundedHistoryAndFinalTransaction','TestObjectMetadataCleanupFinalCommitUnknown']":
        "NAMES=['TestObjectMetadataCleanupIndexMigration']",
    "'b44f46cd:'+path": "'bc8b4581:'+path",
    "('missing',NAMES[:1],True,False,False,False)":
        "('missing',[],True,False,False,False)",
}
for before, after in changes.items():
    assert text.count(before) == 1, 'prior independent control shape changed'
    text = text.replace(before, after)
scope = {'__file__': str(source), '__name__': 'independent_migration_review'}
exec(compile(text, str(source), 'exec'), scope)

# The actual old eight configurations and input lists are identical. Executing
# these modules does not enter main or start any build, fixture or business test.
driver, sup = scope['modules']
old = {'__file__': str(scope['ROOT'] / scope['paths'][0]), '__name__': 'old_review'}
exec(compile(scope['subprocess'].check_output(
    ['git', 'show', 'bc8b4581:' + scope['paths'][0]], cwd=scope['ROOT'], text=True),
    'old_root_driver', 'exec'), old)
assert len(old['TARGETS']) == 8 and len(driver['TARGETS']) == 9
fresh = scope['OUTPUT'] / 'unused-migration-configuration'
assert not fresh.exists()
for selector in old['TARGETS']:
    assert driver['configuration'](str(scope['binary']), selector, str(fresh)) == old['configuration'](str(scope['binary']), selector, str(fresh))
assert driver['input_paths'](str(scope['binary'])) == old['input_paths'](str(scope['binary']))
print('migration delta only; all eight old configurations and input paths unchanged; no main/PG/socket/child')
