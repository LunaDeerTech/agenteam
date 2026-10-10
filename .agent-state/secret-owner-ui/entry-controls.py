#!/usr/bin/env python3
"""Owned Secret browser profile. Actual entry methods; OS/process edges doubled.
The full-byte inverse is against the exact main 18a27db5 donor, not a rebuilt
historical template. No Go, browser, Docker, socket or real resource sampler.
"""
import ast
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
DRIVER = '.agent-state/work-owner-http/root_chain_driver.py'
SUP = '.agent-state/task-planning-recovery/pg_only_supervisor.py'
SELECTOR = '^TestProjectSecretOwnerWeb$'
TOP = 'TestProjectSecretOwnerWeb'
BASE_SHA = {'.agent-state/task-planning-recovery/pg_only_supervisor.py': 'ce09376d0db54c1ef805974c491836cb854f3e23468e0414b0ef31235d00e589',
 '.agent-state/work-owner-http/root_chain_driver.py': '776e6306214722a1eb9f6ca124c12f5e05a3f71d3daffaf3a155c8cee747009c'}
SOURCE_HUNKS = {'.agent-state/task-planning-recovery/pg_only_supervisor.py': [["KNOWLEDGE_UI = '^TestKnowledgeOwnerReadWeb$'\n"
                                                                '\n'
                                                                '\n'
                                                                'def knowledge_ui_results(output):\n'
                                                                "    top = 'TestKnowledgeOwnerReadWeb'\n",
                                                                "KNOWLEDGE_UI = '^TestKnowledgeOwnerReadWeb$'\n"
                                                                "SECRET_OWNER_UI = '^TestProjectSecretOwnerWeb$'\n"
                                                                "UI_CASES = {KNOWLEDGE_UI: ('TestKnowledgeOwnerReadWeb', 'Knowledge'),\n"
                                                                "            SECRET_OWNER_UI: ('TestProjectSecretOwnerWeb', 'SecretOwner')}\n"
                                                                '\n'
                                                                '\n'
                                                                'def knowledge_ui_results(output, selector=KNOWLEDGE_UI):\n'
                                                                '    if selector not in UI_CASES:\n'
                                                                '        return False\n'
                                                                '    top, marker = UI_CASES[selector]\n'],
                                                               ["    browser = re.findall(r'^\\s+\\S+\\.go:[0-9]+: Knowledge Node actual_wait "
                                                                "pid=([1-9][0-9]*) success=(true|false)$', output, re.M)\n",
                                                                "    browser = re.findall(r'^\\s+\\S+\\.go:[0-9]+: ' + re.escape(marker) + r' "
                                                                "Node actual_wait pid=([1-9][0-9]*) success=(true|false)$', output, re.M)\n"],
                                                               ["            and len(waits) == 1 and waits[0][1:] == ('0', KNOWLEDGE_UI)\n"
                                                                "            and len(browser) == 1 and browser[0][1] == 'true'\n",
                                                                "            and len(waits) == 1 and waits[0][1:] == ('0', selector)\n"
                                                                "            and len(browser) == 1 and browser[0][1] == 'true'\n"],
                                                               ['        paths = adapter.knowledge_ui_inputs(args.binary)\n'
                                                                '        return (adapter.knowledge_ui_environment() == '
                                                                'args.knowledge_ui_environment\n',
                                                                '        paths = adapter.knowledge_ui_inputs(args.binary, args.run)\n'
                                                                '        return (adapter.knowledge_ui_environment(args.run) == '
                                                                'args.knowledge_ui_environment\n'],
                                                               ["        KNOWLEDGE_UI: {'TestKnowledgeOwnerReadWeb'},\n",
                                                                '        **{key: {value[0]} for key, value in UI_CASES.items()},\n'],
                                                               ['    if selector == KNOWLEDGE_UI:\n'
                                                                '        complete = knowledge_ui_results(output)\n',
                                                                '    if selector in UI_CASES:\n'
                                                                '        complete = knowledge_ui_results(output, selector)\n'],
                                                               ["    if 'KnowledgeOwnerReadWeb' in args.run and (args.run != KNOWLEDGE_UI or "
                                                                'not args.root_chain):\n'
                                                                "        parser.error('Knowledge UI requires its exact original root-chain "
                                                                "entry')\n",
                                                                '    if any(value[0] in args.run for value in UI_CASES.values()) and (args.run '
                                                                'not in UI_CASES or not args.root_chain):\n'
                                                                "        parser.error('owned UI requires its exact original root-chain "
                                                                "entry')\n"],
                                                               ["    stem = ('ui-' + uuid.uuid4().hex[:16]) if args.run == KNOWLEDGE_UI else "
                                                                "('pg-' + uuid.uuid4().hex)\n",
                                                                "    stem = ('ui-' + uuid.uuid4().hex[:16]) if args.run in UI_CASES else ('pg-' "
                                                                '+ uuid.uuid4().hex)\n'],
                                                               ['    if args.run == KNOWLEDGE_UI:\n'
                                                                '        try:\n'
                                                                '            adapter.knowledge_ui_configuration(directory)\n'
                                                                '            args.knowledge_ui_environment = '
                                                                'adapter.knowledge_ui_environment()\n',
                                                                '    if args.run in UI_CASES:\n'
                                                                '        try:\n'
                                                                '            adapter.knowledge_ui_configuration(directory, args.run)\n'
                                                                '            args.knowledge_ui_environment = '
                                                                'adapter.knowledge_ui_environment(args.run)\n'],
                                                               ['    if args.run == KNOWLEDGE_UI:\n'
                                                                '        inputs = {str(p): adapter.sha(p) for p in '
                                                                'adapter.knowledge_ui_inputs(args.binary)}\n',
                                                                '    if args.run in UI_CASES:\n'
                                                                '        inputs = {str(p): adapter.sha(p) for p in '
                                                                'adapter.knowledge_ui_inputs(args.binary, args.run)}\n'],
                                                               ['            if args.run == KNOWLEDGE_UI and child.returncode is not None:\n',
                                                                '            if args.run in UI_CASES and child.returncode is not None:\n'],
                                                               ['            if args.run == KNOWLEDGE_UI:\n'
                                                                '                same = same and knowledge_ui_same(inputs, args, adapter)\n',
                                                                '            if args.run in UI_CASES:\n'
                                                                '                same = same and knowledge_ui_same(inputs, args, adapter)\n']],
 '.agent-state/work-owner-http/root_chain_driver.py': [['TARGETS = {\n',
                                                        "TARGETS = {\n    '^TestProjectSecretOwnerWeb$': 'internal/central/app',\n"],
                                                       ['def knowledge_ui_assets():\n'
                                                        "    owned = (REPOSITORY / 'output/ai/knowledge-owner-ui').resolve()\n"
                                                        "    dist = Path(os.environ.get(KNOWLEDGE_ENV[0], ''))\n",
                                                        "SECRET_OWNER_UI = '^TestProjectSecretOwnerWeb$'\n"
                                                        '# Only data differs between these two owned browser chains. Resource ownership,\n'
                                                        '# original waits and retirement remain in the existing root supervisor.\n'
                                                        'UI_PROFILES = {\n'
                                                        "    KNOWLEDGE_UI: ('knowledge-owner-ui', 'AGENTEAM_KNOWLEDGE_OWNER_WEB', 'read',\n"
                                                        "                   'knowledge-owner-read', ('common.json', 'knowledge-owner.json', "
                                                        "'knowledge-content.json')),\n"
                                                        "    SECRET_OWNER_UI: ('secret-owner-ui', 'AGENTEAM_SECRET_OWNER_WEB', 'owner',\n"
                                                        "                     'project-secret-owner', ('common.json', "
                                                        "'secret-variables.json')),\n"
                                                        '}\n'
                                                        '\n'
                                                        '\n'
                                                        'def ui_profile(selector):\n'
                                                        '    if selector not in UI_PROFILES:\n'
                                                        "        raise ValueError('one exact owned UI selector required')\n"
                                                        '    profile = UI_PROFILES[selector]\n'
                                                        "    keys = tuple(profile[1] + '_' + suffix for suffix in ('DIST', 'EVIDENCE', "
                                                        "'SCHEMA_PYTHON', 'CASE'))\n"
                                                        '    return profile, keys\n'
                                                        '\n'
                                                        '\n'
                                                        'def knowledge_ui_assets(selector=KNOWLEDGE_UI):\n'
                                                        '    profile, keys = ui_profile(selector)\n'
                                                        "    owned = (REPOSITORY / 'output/ai' / profile[0]).resolve()\n"
                                                        "    dist = Path(os.environ.get(keys[0], ''))\n"],
                                                       ['def knowledge_ui_environment():\n'
                                                        "    values = tuple(os.environ.get(key, '') for key in KNOWLEDGE_ENV)\n"
                                                        "    if (values[2] != str(KNOWLEDGE_PYTHON) or values[3] != 'read'\n",
                                                        'def knowledge_ui_environment(selector=KNOWLEDGE_UI):\n'
                                                        '    profile, keys = ui_profile(selector)\n'
                                                        "    values = tuple(os.environ.get(key, '') for key in keys)\n"
                                                        '    if (values[2] != str(KNOWLEDGE_PYTHON) or values[3] != profile[2]\n'],
                                                       ['def knowledge_ui_configuration(directory):\n'
                                                        '    values = knowledge_ui_environment()\n'
                                                        '    knowledge_ui_assets()\n'
                                                        '    evidence = Path(values[1])\n'
                                                        "    owned = (REPOSITORY / 'output/ai/knowledge-owner-ui').resolve()\n",
                                                        'def knowledge_ui_configuration(directory, selector=KNOWLEDGE_UI):\n'
                                                        '    profile, keys = ui_profile(selector)\n'
                                                        '    values = knowledge_ui_environment(selector)\n'
                                                        '    knowledge_ui_assets(selector)\n'
                                                        '    evidence = Path(values[1])\n'
                                                        "    owned = (REPOSITORY / 'output/ai' / profile[0]).resolve()\n"],
                                                       ['    return dict(zip(KNOWLEDGE_ENV, values))\n', '    return dict(zip(keys, values))\n'],
                                                       ['def knowledge_ui_inputs(binary):\n'
                                                        '    knowledge_ui_environment()\n'
                                                        '    paths = set(input_paths(binary)) | set(root_composition_inputs()) | '
                                                        'set(knowledge_ui_assets())\n'
                                                        "    harness = REPOSITORY / 'tests/account-captcha-web'\n"
                                                        "    paths.update(harness / name for name in ('knowledge-owner-read.config.js', "
                                                        "'package.json', 'package-lock.json',\n"
                                                        "        'e2e/knowledge-owner-read.spec.ts', 'e2e/knowledge-owner-read.native.ts'))\n",
                                                        'def knowledge_ui_inputs(binary, selector=KNOWLEDGE_UI):\n'
                                                        '    profile, _ = ui_profile(selector)\n'
                                                        '    knowledge_ui_environment(selector)\n'
                                                        '    paths = set(input_paths(binary)) | set(root_composition_inputs()) | '
                                                        'set(knowledge_ui_assets(selector))\n'
                                                        "    harness = REPOSITORY / 'tests/account-captcha-web'\n"
                                                        '    stem = profile[3]\n'
                                                        "    paths.update(harness / name for name in (stem + '.config.js', 'package.json', "
                                                        "'package-lock.json',\n"
                                                        "        'e2e/' + stem + '.spec.ts', 'e2e/' + stem + '.native.ts'))\n"
                                                        '    if selector == SECRET_OWNER_UI:\n'
                                                        '        paths.update(REPOSITORY / name for name in (\n'
                                                        "            'internal/central/app/project_secret_owner_web_test.go',\n"
                                                        "            'tests/account-captcha-web/e2e/knowledge-owner-read.native.ts'))\n"
                                                        "        paths.add(REPOSITORY / '.agent-state/secret-owner-ui/entry-controls.py')\n"
                                                        "        paths.add(REPOSITORY / '.agent-state/secret-owner-ui/run.py')\n"],
                                                       ["    paths.update(REPOSITORY / 'api/openapi' / name for name in ('common.json', "
                                                        "'knowledge-owner.json', 'knowledge-content.json'))\n",
                                                        "    paths.update(REPOSITORY / 'api/openapi' / name for name in profile[4])\n"],
                                                       ['    if any(not p.is_file() or p.is_symlink() for p in paths):\n'
                                                        "        raise ValueError('regular complete Knowledge inputs required')\n",
                                                        '    if selector == SECRET_OWNER_UI:\n'
                                                        "        tools = (harness / 'node_modules', REPOSITORY / 'web/node_modules')\n"
                                                        '        paths = {p.resolve(strict=True) if any(p.is_relative_to(tool) for tool in '
                                                        'tools) else p for p in paths}\n'
                                                        '    if any(not p.is_file() or p.is_symlink() for p in paths):\n'
                                                        "        raise ValueError('regular complete Knowledge inputs required')\n"],
                                                       ['def knowledge_ui_input_hash(binary):\n'
                                                        '    digest = hashlib.sha256()\n'
                                                        '    for path in knowledge_ui_inputs(binary):\n',
                                                        'def knowledge_ui_input_hash(binary, selector=KNOWLEDGE_UI):\n'
                                                        '    digest = hashlib.sha256()\n'
                                                        '    for path in knowledge_ui_inputs(binary, selector):\n'],
                                                       ['    if selector == KNOWLEDGE_UI:\n'
                                                        "        plan['knowledge_ui'] = knowledge_ui_configuration(directory)\n",
                                                        '    if selector in UI_PROFILES:\n'
                                                        "        plan['knowledge_ui'] = knowledge_ui_configuration(directory, selector)\n"],
                                                       ['    if args.run == KNOWLEDGE_UI:\n'
                                                        '        prepare_history_go_environment(directory, env)\n'
                                                        "        ui = plan['knowledge_ui']\n"
                                                        "        Path(ui['AGENTEAM_KNOWLEDGE_OWNER_WEB_EVIDENCE']).mkdir(mode=0o700)\n"
                                                        '        env.update(ui)\n'
                                                        "        env.update({'AGENTEAM_AUTH_WEB_RUNTIME': str(runtime),\n"
                                                        "                    'AGENTEAM_KNOWLEDGE_OWNER_WEB_INPUT_HASH': "
                                                        'knowledge_ui_input_hash(args.test_binary),\n',
                                                        '    if args.run in UI_PROFILES:\n'
                                                        '        prepare_history_go_environment(directory, env)\n'
                                                        '        profile, keys = ui_profile(args.run)\n'
                                                        "        ui = plan['knowledge_ui']\n"
                                                        '        Path(ui[keys[1]]).mkdir(mode=0o700)\n'
                                                        '        env.update(ui)\n'
                                                        "        env.update({'AGENTEAM_AUTH_WEB_RUNTIME': str(runtime),\n"
                                                        "                    profile[1] + '_INPUT_HASH': "
                                                        'knowledge_ui_input_hash(args.test_binary, args.run),\n']]}


def inverse(name, source):
    if name not in BASE_SHA or not isinstance(source, str):
        raise ValueError('unknown shared source')
    for old, new in reversed(SOURCE_HUNKS[name]):
        if source.count(new) != 1:
            raise ValueError('unknown or ambiguous Secret UI profile delta')
        source = source.replace(new, old, 1)
    if hashlib.sha256(source.encode()).hexdigest() != BASE_SHA[name]:
        raise ValueError('unknown shared baseline change')
    return source


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def output(selector=SELECTOR, marker='SecretOwner'):
    top = selector[1:-1]
    return ('=== RUN   ' + top + '\n'
            + '    owned.go:100: ' + marker + ' Node actual_wait pid=43 success=true\n'
            + '--- PASS: ' + top + ' (0.1s)\n'
            + 'D03 explicit test actual_wait pid=42 code=0 selector=' + selector + '\n')


class SecretUIEntryControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.driver, cls.sup = load('secret_ui_driver', DRIVER), load('secret_ui_sup', SUP)

    def test_inverse_and_existing_data_profiles(self):
        legacy = load('secret_ui_install_control', '.agent-state/skill-installation/entry-controls.py')
        metadata = load('secret_ui_metadata_control', '.agent-state/agent-configuration-metadata/metadata-entry-controls.py')
        for name in BASE_SHA:
            source = (ROOT / name).read_text()
            restored = inverse(name, source)
            self.assertEqual(hashlib.sha256(restored.encode()).hexdigest(), BASE_SHA[name])
            ast.parse(restored)
            legacy.inverse(name, source)
            metadata.inverse(name, source)
            for bad in (source + '\n# unknown\n', source.replace('540', '541', 1)):
                if bad != source:
                    with self.assertRaises(ValueError): inverse(name, bad)
            for old, new in SOURCE_HUNKS[name]:
                for bad in (source.replace(new, old, 1), source + new):
                    with self.assertRaises(ValueError): inverse(name, bad)
        self.assertEqual(set(self.driver.UI_PROFILES), set(self.sup.UI_CASES))
        self.assertEqual(set(self.driver.METADATA_INPUTS), set(self.sup.METADATA_GROUPS))
        self.assertEqual(self.driver.TARGETS[SELECTOR], 'internal/central/app')
        self.assertEqual(self.sup.budgets(True), (540,60))

    def test_exact_results_and_original_mode_guards(self):
        for selector, (top, marker) in self.sup.UI_CASES.items():
            good=output(selector,marker)
            self.assertTrue(self.sup.knowledge_ui_results(good,selector))
            lines=good.splitlines(True)
            for line in lines:
                for bad in (good.replace(line,''),good+line):
                    self.assertFalse(self.sup.knowledge_ui_results(bad,selector))
            for bad in (good.replace('success=true','success=false'),good.replace('code=0','code=1'),
                        good.replace('PASS:','SKIP:'),good.replace('PASS:','FAIL:'),
                        good.replace(marker+' Node','Other Node'),good+'FAIL\n',good+'=== RUN   TestUnknown\n'):
                self.assertFalse(self.sup.knowledge_ui_results(bad,selector))
        args=['sup','--driver','/unused/d','--binary','/unused/b','--output','/unused/o','--run']
        for selector, root in ((SELECTOR,False),(TOP,True),(SELECTOR+'x',True),('^'+TOP+'(Extra)?$',True)):
            with patch.object(sys,'argv',args+[selector]+(['--root-chain'] if root else [])), patch('sys.stderr',io.StringIO()), \
                 patch.object(self.sup,'budgets') as budget, patch.object(self.sup,'root_adapter') as adapter, \
                 patch.object(self.sup.subprocess,'Popen') as spawn:
                with self.assertRaises(SystemExit): self.sup.main()
                budget.assert_not_called();adapter.assert_not_called();spawn.assert_not_called()
        class OriginalBoundary(Exception): pass
        for selector,root in ((SELECTOR,True),(self.sup.KNOWLEDGE_UI,True),
                              (self.sup.SKILL_HTTP_PG,False),(self.sup.SECRET_ROOT,True),
                              ('^TestSkillInstallationPersistentObject$',True)):
            with patch.object(sys,'argv',args+[selector]+(['--root-chain'] if root else [])), \
                 patch.object(self.sup,'budgets',side_effect=OriginalBoundary) as budget:
                with self.assertRaises(OriginalBoundary): self.sup.main()
                budget.assert_called_once_with(root)

    def test_real_manifest_observer_and_all_original_tails(self):
        with tempfile.TemporaryDirectory(prefix='secret-ui-observe-') as tmp:
            root=Path(tmp);runtime=root/'runtime';runtime.mkdir()
            record={'kind':'work-owner-root-chain','resources':[], 'directories':[str(runtime/name) for name in ('object','outbound','pg')]}
            index=0
            for label,kinds in (('agenteam.d05.objectfixture',('container','network')),('agenteam.d04.networkfixture',('container','network')),('agenteam.d03.fixture',('container','container','network'))):
                for kind in kinds:
                    index+=1;record['resources'].append({'kind':kind,'id':format(index,'064x'),'label':label,'nonce':'1'*32})
            manifest=root/'owned.json';manifest.write_text(json.dumps(record));manifest.chmod(0o600)
            log=root/'case.log';log.write_text(output())
            with patch.object(self.sup,'exact_absent',return_value=True) as absent, patch.object(self.sup.time,'sleep'):
                self.assertTrue(self.sup.observe_root_chain(root,io.StringIO(),log,SELECTOR));self.assertEqual(absent.call_count,14)
                for bad in (output().replace('success=true','success=false'),output().replace('code=0','code=1')):
                    log.write_text(bad);self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,SELECTOR))
                log.write_text(output());p=runtime/'pg';p.mkdir();self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,SELECTOR));p.rmdir()
                absent.return_value=False;self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,SELECTOR));absent.return_value=True
                manifest.chmod(0o644);self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,SELECTOR));manifest.chmod(0o600)
                (runtime/'held').write_text('pending');self.assertFalse(self.sup.observe_root_chain(root,io.StringIO(),log,SELECTOR))

    def test_driver_original_exec_and_private_environment(self):
        class ExecBoundary(Exception): pass
        with tempfile.TemporaryDirectory(prefix='secret-ui-exec-') as tmp:
            root=Path(tmp);directory=root/'owned';evidence=root/'evidence'
            profile,keys=self.driver.ui_profile(SELECTOR)
            values=(str(root/'dist'),str(evidence),str(self.driver.KNOWLEDGE_PYTHON),'owner')
            plan={'binary':str(root/'candidate'),'cwd':str(root/'internal/central/app'),'directory':str(directory),'runtime':str(directory/'runtime'),'knowledge_ui':dict(zip(keys,values))}
            def execute(file,argv,env):
                self.assertEqual(file,'/bin/sh');self.assertEqual(argv,['/bin/sh','scripts/test-objects.sh','--run',SELECTOR])
                self.assertEqual(env['AGENTEAM_SECRET_OWNER_WEB_INPUT_HASH'],'a'*64)
                self.assertEqual(env['AGENTEAM_AUTH_WEB_RUNTIME'],str(directory/'runtime'))
                self.assertEqual((Path(env['XDG_CONFIG_HOME'])/'go/telemetry/mode').read_text(),'off\n')
                self.assertTrue(evidence.is_dir());self.assertTrue(env['PATH'].startswith(str(self.driver.KNOWLEDGE_NODE.parent)+os.pathsep))
                for name in ('TEST_TELEMETRY_DIR','GO_TELEMETRY_CHILD','GO_TELEMETRY_CHILD_UPLOAD'):self.assertNotIn(name,env)
                raise ExecBoundary()
            with patch.object(sys,'argv',['driver','--test-binary',plan['binary'],'--run',SELECTOR,'--directory',str(directory)]), \
                 patch.object(self.driver,'configuration',return_value=plan),patch.object(self.driver,'knowledge_ui_input_hash',return_value='a'*64), \
                 patch.object(self.driver.os,'chdir'),patch.object(self.driver.os,'execve',side_effect=execute), \
                 patch.dict(os.environ,{n:'bad' for n in ('TEST_TELEMETRY_DIR','GO_TELEMETRY_CHILD','GO_TELEMETRY_CHILD_UPLOAD')}):
                with self.assertRaises(ExecBoundary):self.driver.main()

    def test_actual_collector_reenumerates_scope_tools_and_dist(self):
        with tempfile.TemporaryDirectory(prefix='secret-ui-inputs-') as tmp:
            root=Path(tmp).resolve();dist=root/'output/ai/secret-owner-ui/dist';dist.mkdir(parents=True)
            names=['candidate','internal/central/app/project_secret_owner_web_test.go','internal/central/app/original_test.go',
                   'tests/account-captcha-web/project-secret-owner.config.js','tests/account-captcha-web/package.json','tests/account-captcha-web/package-lock.json',
                   'tests/account-captcha-web/e2e/project-secret-owner.spec.ts','tests/account-captcha-web/e2e/project-secret-owner.native.ts','tests/account-captcha-web/e2e/knowledge-owner-read.native.ts',
                   'web/src/new.vue','web/package.json','web/package-lock.json','web/node_modules/typescript/package.json','web/node_modules/typescript/lib/typescript.js',
                   'api/openapi/common.json','api/openapi/secret-variables.json','.agent-state/secret-owner-ui/entry-controls.py',
                   '.agent-state/secret-owner-ui/run.py',
                   'output/ai/secret-owner-ui/dist/index.html','output/ai/secret-owner-ui/dist/assets/app.js','tools/node','tools/python','usr/bin/chromium','usr/lib/chromium/chromium','etc/chromium.d/setting']
            for package in ('@playwright/test','playwright','playwright-core'):
                names.extend('tests/account-captcha-web/node_modules/'+package+'/'+n for n in ('package.json','runtime.js'))
            names.append('tests/account-captcha-web/node_modules/@playwright/test/cli.js')
            for name in names:
                p=root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text('{"version":"1.56.1"}' if name.endswith('package.json') else 'controlled');p.chmod(0o700)
            profile,keys=self.driver.ui_profile(SELECTOR)
            values=(str(dist),str(root/'output/ai/secret-owner-ui/evidence'),str(root/'tools/python'),'owner')
            def scoped_path(value):
                p=Path(value)
                return root/str(p).lstrip('/') if str(p).startswith(('/usr/','/etc/')) else p
            with patch.object(self.driver,'REPOSITORY',root),patch.object(self.driver,'Path',side_effect=scoped_path), \
                 patch.object(self.driver,'KNOWLEDGE_NODE',root/'tools/node'),patch.object(self.driver,'KNOWLEDGE_PYTHON',root/'tools/python'), \
                 patch.object(self.driver,'input_paths',return_value=[root/'candidate']),patch.dict(os.environ,dict(zip(keys,values))):
                paths=self.driver.knowledge_ui_inputs(root/'candidate',SELECTOR)
                self.assertEqual(set(paths),{root/name for name in names})
                inputs={str(p):self.driver.sha(p) for p in paths};args=SimpleNamespace(binary=root/'candidate',run=SELECTOR,knowledge_ui_environment=values)
                self.assertTrue(self.sup.knowledge_ui_same(inputs,args,self.driver))
                for rel in ('internal/central/app/later_test.go','web/src/newfile.ts','output/ai/secret-owner-ui/dist/assets/later.js','tests/account-captcha-web/node_modules/playwright/later.js'):
                    p=root/rel;p.write_text('later');self.assertFalse(self.sup.knowledge_ui_same(inputs,args,self.driver));p.unlink()
                for rel in names:
                    p=root/rel;original=p.read_text();p.write_text('changed');self.assertFalse(self.sup.knowledge_ui_same(inputs,args,self.driver));p.write_text(original)
                for rel in ('internal/central/app/project_secret_owner_web_test.go','tests/account-captcha-web/e2e/knowledge-owner-read.native.ts','output/ai/secret-owner-ui/dist/assets/app.js'):
                    p=root/rel;original=p.read_text();p.unlink();self.assertFalse(self.sup.knowledge_ui_same(inputs,args,self.driver));p.symlink_to(root/'candidate');self.assertFalse(self.sup.knowledge_ui_same(inputs,args,self.driver));p.unlink();p.write_text(original)
                with patch.dict(os.environ,{keys[3]:'unknown'}):self.assertFalse(self.sup.knowledge_ui_same(inputs,args,self.driver))
                with self.assertRaises(ValueError):self.driver.knowledge_ui_inputs(root/'candidate','^TestUnknown$')

    def test_outer_records_original_empty_sets_after_actual_wait(self):
        runner = load('secret_ui_outer', '.agent-state/secret-owner-ui/run.py')
        for free in (6 * 1024 ** 3, 0):
            with self.subTest(free=free), tempfile.TemporaryDirectory(prefix='secret-ui-launch-') as tmp:
                root = Path(tmp)
                owned = root / 'output/ai/secret-owner-ui'
                owned.mkdir(parents=True)
                source = root / 'input'
                source.write_text('controlled')
                waited = []
                class Child:
                    pid = 123
                    returncode = None
                    def wait(self):
                        waited.append(self.pid)
                        self.returncode = 0
                        return 0
                adapter = {'configuration': lambda *a: None,
                           'knowledge_ui_inputs': lambda *a: [source],
                           'sha': lambda p: hashlib.sha256(p.read_bytes()).hexdigest()}
                supervisor = {'tcp': lambda: set(), 'descendants': lambda pid: set()}
                def start(command, **kwargs):
                    self.assertEqual(command[command.index('--run') + 1], SELECTOR)
                    self.assertIn('--root-chain', command)
                    env = kwargs['env']
                    self.assertEqual((Path(env['XDG_CONFIG_HOME']) / 'go/telemetry/mode').read_text(), 'off\n')
                    self.assertEqual(list(Path(env['DOCKER_CONFIG']).iterdir()), [])
                    for key in ('TEST_TELEMETRY_DIR','GO_TELEMETRY_CHILD','GO_TELEMETRY_CHILD_UPLOAD'):
                        self.assertNotIn(key, env)
                    return Child()
                with patch.object(runner, 'ROOT', root), patch.object(runner, 'OWNED', owned), \
                     patch.object(sys, 'argv', ['run.py', '--attempt', '98']), \
                     patch.object(runner.shutil, 'disk_usage', return_value=SimpleNamespace(free=free)), \
                     patch.object(runner.shutil, 'which', return_value='/usr/local/bin/docker'), \
                     patch.object(runner.runpy, 'run_path', side_effect=[adapter, supervisor]), \
                     patch.object(runner.ctypes, 'CDLL', return_value=SimpleNamespace(prctl=lambda *a: 0)), \
                     patch.object(runner.subprocess, 'Popen', side_effect=start) as spawn, \
                     patch.object(runner.signal, 'signal'), patch.object(runner.os, 'kill', side_effect=AssertionError('unexpected kill')), \
                     patch.object(runner.os, 'waitpid', side_effect=ChildProcessError), \
                     patch.object(runner.time, 'sleep'), patch.dict(os.environ, {'GO_TELEMETRY_CHILD':'bad'}), \
                     patch.object(sys, 'stdout', io.StringIO()):
                    code = runner.main()
                record = json.loads((owned / 'native-98-control/result.json').read_text())
                self.assertEqual(code, 0 if free else 1)
                self.assertEqual(record['exit'], code)
                if free:
                    self.assertEqual(waited, [123])
                    self.assertTrue(record['actual_supervisor_wait'])
                    self.assertEqual(record['outer_descendants'], [[], []])
                    self.assertEqual(record['outer_tcp_empty_observations'], 2)
                    self.assertEqual(record['adopted'], [])
                else:
                    spawn.assert_not_called()
                    self.assertFalse(record['actual_supervisor_wait'])

if __name__ == '__main__':unittest.main()
