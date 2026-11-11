"""真实 Compose 表示差异与挂载漂移边界；不创建服务或联网。"""
import ast
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile

source=Path(sys.argv[1]).read_text(encoding='utf-8')
tree=ast.parse(source)
names={'normalize_compose_merge','compose_merge_matches'}
scope={'copy':copy}
exec(compile(ast.Module(body=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names],type_ignores=[]),'delivery-compose-functions','exec'),scope)
matches=scope['compose_merge_matches']

def command(args):
    result=subprocess.run(args,capture_output=True,text=True)
    assert result.returncode==0,result.stderr
    return result.stdout

with tempfile.TemporaryDirectory(prefix='mx-compose-compat-') as work:
    root=Path(work)
    paths=[root/'old',root/'state']
    for path in paths:path.mkdir()
    base={'services':{'cpa':{'image':'synthetic/compose-config-only:1','volumes':[{'type':'bind','source':paths[0].as_posix(),'target':'/old'}]}}}
    added={'type':'bind','source':paths[1].as_posix(),'target':'/state','bind':{'create_host_path':True}}
    override={'services':{'cpa':{'volumes':[added]}}}
    (root/'base.json').write_text(json.dumps(base))
    (root/'candidate.json').write_text(json.dumps(override))
    args=['docker','compose','--project-directory',str(root),'-p','mx-compose-config-only','-f',str(root/'base.json')]
    expected=json.loads(command(args+['config','--format','json']))
    expected['services']['cpa']['volumes'].append(copy.deepcopy(added))
    actual=json.loads(command(args+['-f',str(root/'candidate.json'),'config','--format','json']))
    for value in (expected,actual):
        value['services']['cpa']['volumes']=sorted(value['services']['cpa']['volumes'],key=lambda item:item['target'])
    original=copy.deepcopy(expected)
    assert matches(expected,actual),'actual_compose_merge_rejected'
    explicit_old_comparison=expected==actual
    override['services']['cpa']['volumes'][0].pop('bind')
    (root/'candidate.json').write_text(json.dumps(override))
    default_actual=json.loads(command(args+['-f',str(root/'candidate.json'),'config','--format','json']))
    default_actual['services']['cpa']['volumes']=sorted(default_actual['services']['cpa']['volumes'],key=lambda item:item['target'])
    assert expected!=default_actual,'default_representation_did_not_reproduce_old_rejection'
    assert matches(expected,default_actual),'actual_default_representation_rejected'
    assert expected==original,'comparison_mutated_input'
    candidates=expected['services']['cpa']['volumes']
    index=next(i for i,item in enumerate(candidates) if item['target']=='/state')
    def variant(change):
        value=copy.deepcopy(expected)
        change(value['services']['cpa']['volumes'][index])
        return value
    accepted=[('default_omitted',lambda item:item.pop('bind')),('empty_bind',lambda item:item.update(bind={})),('true_with_other_option',lambda item:item.update(bind={'create_host_path':True,'propagation':'rprivate'}))]
    for name,change in accepted[:2]:assert matches(expected,variant(change)),name
    with_option=variant(accepted[2][1])
    without_default=copy.deepcopy(with_option)
    without_default['services']['cpa']['volumes'][index]['bind'].pop('create_host_path')
    assert matches(with_option,without_default),'other_option_preserved_with_default'
    rejected=[('false',lambda item:item.update(bind={'create_host_path':False})),('source',lambda item:item.update(source=str(root/'different'))),('target',lambda item:item.update(target='/different')),('read_only',lambda item:item.update(read_only=True)),('propagation',lambda item:item.update(bind={'propagation':'rshared'})),('selinux',lambda item:item.update(bind={'selinux':'z'})),('type',lambda item:item.update(type='volume'))]
    for name,change in rejected:assert not matches(expected,variant(change)),name+'_drift_accepted'
    false_value=variant(rejected[0][1])
    assert matches(false_value,false_value),'false_identical_rejected'
    assert not matches(false_value,variant(lambda item:item.update(bind={}))), 'false_default_accepted'
    print(json.dumps({'compose_version':command(['docker','compose','version']).strip(),'actual_merge_accepted':True,'explicit_default_old_strict_comparison_passed':explicit_old_comparison,'default_representation_old_strict_comparison_rejected':expected!=default_actual,'default_representation_new_comparison_passed':True,'actual_default_mount':next(item for item in default_actual['services']['cpa']['volumes'] if item['target']=='/state'),'default_cases':3,'rejected_drift_cases':[name for name,_ in rejected],'identical_false_preserved':True,'inputs_unchanged':expected==original,'services_created':False,'result':'passed'},sort_keys=True),flush=True)
