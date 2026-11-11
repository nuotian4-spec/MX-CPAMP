"""仅核本轮配置合并和站点前置条件，不访问外部服务。"""
import copy
import importlib.util
import json

spec=importlib.util.spec_from_file_location('release','/delivery/bps-final-release.py')
r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
cases=0
assert r.resolve_state('~\\bps-state','/CLIProxyAPI')=='/root/bps-state'
assert r.resolve_state('~/bps-state','/CLIProxyAPI')=='/root/bps-state'
assert r.resolve_state('~','/CLIProxyAPI')=='/root'
assert r.resolve_state('relative-state','/CLIProxyAPI')=='/CLIProxyAPI/relative-state'

for source,transport in [({},'native'),({'bps-protocol':{'image-transport':'relay','image-relay-base-url':r.RELAY_ORIGIN+'/'}},'relay'),({'bps-protocol':{'enabled':False,'image-transport':'relay','image-relay-base-url':r.RELAY_ORIGIN,'state-dir':'/old'},'upstream':{'bps':{'protocol':{'enabled':None,'images-enabled':False,'image-transport':None,'image-relay-base-url':None,'state-dir':None}}}},'native')]:
    original=copy.deepcopy(source)
    source['unrelated']={'preserve':False}
    candidate=r.candidate_config(source)
    actual=r.effective_protocol(candidate)
    assert actual['image-transport']==transport and actual['image-relay-base-url']==r.RELAY_ORIGIN and actual['state-dir']=='/var/lib/cpa/bps-state'
    assert candidate['unrelated']==source['unrelated'] and source=={**original,'unrelated':{'preserve':False}}
    assert not any(key in candidate.get('bps-protocol',{}) for key in ['image-transport','image-relay-base-url','state-dir'])
    cases+=1

for source in [{'upstream':None},{'upstream':{'bps':None}},{'upstream':{'bps':{'protocol':None}}},{'bps-protocol':{'image-transport':'unknown'}},{'bps-protocol':{'image-relay-base-url':'https://other.invalid'}},{'bps-protocol':{'image-relay-base-url':r.RELAY_ORIGIN+'//'}},{'bps-protocol':{'image-transport':'relay'}}]:
    try:r.candidate_config(source)
    except r.Failure:cases+=1
    else:raise AssertionError('invalid_protocol_accepted')

document={'apps':{'http':{'servers':{'srv0':{'listen':[':443'],'routes':[{'match':[{'host':[r.RELAY_HOST]}],'terminal':True,'handle':[{'handler':'subroute','routes':[{'handle':[{'handler':'reverse_proxy','upstreams':[{'dial':'cli-proxy-api:8317'}]}]}]}]}]}}}}}
assert r.caddy_site(document)
document['apps']['http']['servers']['srv0']['routes'][0]['handle'][0]['routes'][0]['handle'][0]['upstreams'][0]['dial']='cpa-manager-plus:18317'
try:r.caddy_site(document)
except r.Failure as error:assert str(error)=='relay_site_upstream_changed'
else:raise AssertionError('wrong_cpa_site_accepted')
print(json.dumps({'protocol_cases':cases,'state_home_and_cwd_cases':4,'candidate_no_source_mutation':True,'site_upstream_mismatch_rejected':True,'result':'passed'}))
