"""仅替换隔离事实常量，调用原交付main。"""
import importlib.util
import json
import os
from pathlib import Path
import sys

spec=importlib.util.spec_from_file_location('release','/delivery/bps-final-release.py')
r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
r.ROOT=Path(os.environ['FIXTURE_ROOT']);r.PROJECT=os.environ['FIXTURE_PROJECT'];r.STATE=r.ROOT/'.mx-upgrade-bps-relay-20261111'
r.BASELINE=json.loads((r.ROOT/'fixture-baseline.json').read_text())
try:
    r.main(sys.argv[1:])
except Exception as error:
    code=str(error) if isinstance(error,r.Failure) and r.re.fullmatch('[a-z_]+',str(error)) else 'operation_failed'
    stage=(r.STATE/'stage').read_text().strip() if (r.STATE/'stage').exists() else 'initial'
    print('release_failed stage='+stage+' code='+code,flush=True)
    if code in ['command_failed','compose_process_drift','candidate_merge_changed']:
        trace=error.__traceback__
        while trace:
            if trace.tb_frame.f_code.co_name=='command':
                args=trace.tb_frame.f_locals.get('args',[])
                result=trace.tb_frame.f_locals.get('result')
                print('fixture_command='+json.dumps(args),flush=True)
                if result is not None:print('fixture_command_error='+result.stderr,flush=True)
            if trace.tb_frame.f_code.co_name=='compose_matches_live' and code=='compose_process_drift':
                local=trace.tb_frame.f_locals
                field=local.get('field')
                print('fixture_process_difference='+json.dumps({'service':local.get('service'),'field':field,'actual':local.get('cc',{}).get(field),'expected':local.get('expected'),'default':local.get('default')}),flush=True)
            if trace.tb_frame.f_code.co_name=='prepare' and code=='candidate_merge_changed':
                def differences(left,right,path=''):
                    if type(left)!=type(right):return [path+':'+type(left).__name__+'!='+type(right).__name__]
                    if isinstance(left,dict):
                        return [path+'/'+key+':missing' for key in left.keys()^right.keys()]+[part for key in left.keys()&right.keys() for part in differences(left[key],right[key],path+'/'+key)]
                    if isinstance(left,list):
                        return ([path+':length'] if len(left)!=len(right) else [])+[part for index,(a,b) in enumerate(zip(left,right)) for part in differences(a,b,path+'/'+str(index))]
                    return [] if left==right else [path+':value']
                local=trace.tb_frame.f_locals
                print('fixture_merge_difference='+json.dumps(differences(local['expected'],local['rendered'])),flush=True)
            trace=trace.tb_next
    sys.exit(1)
