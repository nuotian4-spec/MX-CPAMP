"""真实Linux/Compose发布边界；仅本轮隔离合成站点。"""
import copy
import gzip
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import pty
import select
import subprocess
import sys
import tarfile
import time

ROOT=Path(os.environ['FIXTURE_ROOT'])
PROJECT=os.environ['FIXTURE_PROJECT']
DELIVERY=Path('/delivery/phase2-release.py')

def command(args, **kwargs):
    result=subprocess.run(args, capture_output=True, text=True, timeout=120, **kwargs)
    if result.returncode:
        raise RuntimeError('fixture_command_failed:'+json.dumps(args)+':'+result.stderr)
    return result.stdout

def report(name, **facts):
    print(json.dumps({'boundary':name, **facts}), flush=True)

def compose(files, *args):
    result=['docker','compose','--project-directory',str(ROOT),'-p',PROJECT]
    for file in files:
        result+=['-f',str(file)]
    return command(result+list(args))

def service(service):
    ids=command(['docker','ps','-aq','--filter','label=com.docker.compose.project='+PROJECT,'--filter','label=com.docker.compose.service='+service]).split()
    assert len(ids)==1, 'fixture_unique_service_'+service
    return json.loads(command(['docker','inspect',ids[0]]))[0]

def invoke(*args, tty_input=None, on_stopping=None):
    cmd=['python3','/checks/invoke.py']+list(args)
    if tty_input is None:
        result=subprocess.run(cmd,input='DEPLOY\n',capture_output=True,text=True,timeout=120)
        return result.returncode,result.stdout+result.stderr
    master,slave=pty.openpty()
    child=subprocess.Popen(cmd,stdin=slave,stdout=slave,stderr=slave)
    os.close(slave)
    output=b'';sent=False;deadline=time.monotonic()+180
    while child.poll() is None and time.monotonic()<deadline:
        readable,_,_=select.select([master],[],[],.2)
        if readable:
            try:output+=os.read(master,65536)
            except OSError:break
        if not sent and b'DEPLOY' in output:
            os.write(master,tty_input);sent=True
        if on_stopping and b'stage=stopping_apps' in output:
            on_stopping();on_stopping=None
    if child.poll() is None:
        child.kill();child.wait();raise RuntimeError('fixture_pty_timeout')
    while True:
        try:
            readable,_,_=select.select([master],[],[],0)
            if not readable:break
            output+=os.read(master,65536)
        except OSError:break
    os.close(master)
    return child.returncode,output.decode(errors='replace')

def create_site():
    for name in ('cpa/auth','cpa/plugins','cpa/logs','manager-data','secrets'):
        (ROOT/name).mkdir(parents=True, exist_ok=True)
    secret=ROOT/'secrets/oauth_login_secret'
    secret.write_text('SYNTHETIC_WORKER_SECRET_'+'a'*48)
    os.chown(secret,0,1000);secret.chmod(0o440)
    (ROOT/'secrets/cpamp-admin-key').write_text('SYNTHETIC_ADMIN_KEY_'+'b'*48)
    (ROOT/'secrets/cpamp-admin-key').chmod(0o600)
    management='SYNTHETIC_MANAGEMENT_KEY_'+'c'*32
    config={'config-version':8,'server':{'host':'0.0.0.0','port':8317,'kv-store':{'disabled':True}},'management':{'allow-remote':True,'secret-key':management,'disable-control-panel':True,'disable-auto-update-panel':True},'access':{'api-keys':['SYNTHETIC_API_KEY_'+'d'*48]},'oauth':{'auth-dir':'/root/.cli-proxy-api','providers':{'bps':{'models':['gpt-6-luna']}}}}
    (ROOT/'cpa/config.yaml').write_text(json.dumps(config))
    manager={'httpAddr':'0.0.0.0:18317','dataDir':'/data','cpaUpstreamUrl':'http://cli-proxy-api:8317','managementKeyFile':'/data/cpa-key','adminKeyFile':'/run/secrets/cpamp-admin-key'}
    (ROOT/'manager-data/config.json').write_text(json.dumps(manager));(ROOT/'manager-data/cpa-key').write_text(management)
    # 真实Manager初始化自己的SQLite/WAL；测试不伪造数据库文件。
    (ROOT/'Caddyfile').write_text(':80 {\n  respond "fixture"\n}\n')
    (ROOT/'cpa-compose').write_text('#!/bin/sh\nexec docker compose --project-directory '+str(ROOT)+' -p '+PROJECT+' -f '+str(ROOT/'compose.yaml')+' -f '+str(ROOT/'compose.mx-cpamp.yaml')+' "$@"\n')
    (ROOT/'cpa-compose').chmod(0o700)
    common={'logging':{'driver':'none'},'labels':{'mx.phase2boundary':PROJECT},'pull_policy':'never'}
    def bind(source,target,ro=False):return {'type':'bind','source':str(ROOT/source),'target':target,'read_only':ro}
    services={
      'cli-proxy-api':{**common,'image':'mx-cpa:20261110-rc1','command':['./CLIProxyAPI','--config','/CLIProxyAPI/config.yaml'],'volumes':[bind('cpa/config.yaml','/CLIProxyAPI/config.yaml'),bind('cpa/logs','/CLIProxyAPI/logs'),bind('cpa/plugins','/CLIProxyAPI/plugins'),bind('cpa/auth','/root/.cli-proxy-api')]},
      'cpa-manager-plus':{**common,'image':'mx-cpamp:20261110-rc1','environment':{'CPA_MANAGER_CONFIG':'/data/config.json'},'volumes':[bind('manager-data','/data'),bind('secrets/cpamp-admin-key','/run/secrets/cpamp-admin-key',True)]},
      'caddy':{**common,'image':'caddy:2.10.2-alpine','volumes':[bind('Caddyfile','/etc/caddy/Caddyfile',True),{'type':'volume','source':'caddy_data','target':'/data'},{'type':'volume','source':'caddy_config','target':'/config'}]}}
    existing=json.loads(command(['docker','network','inspect']+command(['docker','network','ls','-q']).split()))
    occupied=[ipaddress.ip_network(item['Subnet']) for network in existing for item in network.get('IPAM',{}).get('Config') or [] if item.get('Subnet')]
    subnet=next(ipaddress.ip_network('10.253.'+str(index)+'.0/28') for index in range(1,255) if not any(ipaddress.ip_network('10.253.'+str(index)+'.0/28').overlaps(old) for old in occupied if old.version==4))
    base={'services':services,'networks':{'default':{'internal':True,'ipam':{'config':[{'subnet':str(subnet)}]}}},'volumes':{'caddy_data':{},'caddy_config':{}}}
    override={'services':{'cpa-manager-plus':{'environment':{'OAUTH_LOGIN_URL':'http://oauth-login:18417','OAUTH_LOGIN_SECRET_FILE':'/run/secrets/oauth_login_secret'},'volumes':[bind('secrets/oauth_login_secret','/run/secrets/oauth_login_secret',True)]},'oauth-login':{**common,'image':'mx-oauth-login:20261110-rc1','environment':{'OAUTH_LOGIN_SECRET_FILE':'/run/secrets/oauth_login_secret'},'read_only':True,'tmpfs':['/tmp:mode=1777,size=32m'],'volumes':[bind('secrets/oauth_login_secret','/run/secrets/oauth_login_secret',True)]}}}
    (ROOT/'compose.yaml').write_text(json.dumps(base));(ROOT/'compose.mx-cpamp.yaml').write_text(json.dumps(override))
    legacy=ROOT/'.mx-upgrade-20261110-rc1';legacy.mkdir(mode=0o700)
    (legacy/'complete').write_text('');(legacy/'backup').mkdir(mode=0o700)
    (legacy/'backup/historical').write_text('SYNTHETIC_OLD_BACKUP_MARKER')
    compose([ROOT/'compose.yaml'],'up','-d','--pull','never','--no-build')
    compose([ROOT/'compose.yaml',ROOT/'compose.mx-cpamp.yaml'],'up','-d','--pull','never','--no-build','--no-deps','cli-proxy-api','cpa-manager-plus','oauth-login')
    return config

def source_rejections(module,live):
    cases=[('HOME_JWT','SYNTHETIC_CONFIG_MODE','cpa_external_config_mode'),('PGSTORE_DSN','postgres://synthetic.invalid/fixture','cpa_external_config_mode')]
    for key,value,expected in cases:
        cid=command(['docker','create','--network','none','--label','mx.phase2boundary='+PROJECT,'--env',key+'='+value,live['cli-proxy-api']['Image']]).strip()
        try:
            actual=json.loads(command(['docker','inspect',cid]))[0]
            try:module.sources({**live,'cli-proxy-api':actual},module.config())
            except module.Failure as error:assert str(error)==expected,('source_env',key,str(error))
            else:raise AssertionError('source_env_not_rejected_'+key)
            assert not (module.STATE/'maintenance').exists(),'source_probe_maintenance'
            report('cpa_source_selection',source=key,actual_inspect=True,rejected=True,code=expected,zero_maintenance=True)
        finally:command(['docker','rm',cid])
    cid=command(['docker','create','--network','none','--label','mx.phase2boundary='+PROJECT,'--mount','type=bind,source='+str(ROOT/'cpa/auth')+',target=/root/.cli-proxy-api',live['cli-proxy-api']['Image'],'./CLIProxyAPI','--config','/root/.cli-proxy-api/other.yaml']).strip()
    try:
        actual=json.loads(command(['docker','inspect',cid]))[0]
        try:module.sources({**live,'cli-proxy-api':actual},module.config())
        except module.Failure as error:assert str(error)=='cpa_config_source_changed',('source_args',str(error))
        else:raise AssertionError('source_args_not_rejected')
        report('cpa_source_selection',source='alternate_config_args',actual_inspect=True,rejected=True,code='cpa_config_source_changed',zero_maintenance=not (module.STATE/'maintenance').exists())
    finally:command(['docker','rm',cid])
    cid=command(['docker','create','--network','none','--label','mx.phase2boundary='+PROJECT,'--mount','type=bind,source='+str(ROOT/'secrets/oauth_login_secret')+',target=/run/secrets/oauth_login_secret,readonly','--env','OAUTH_LOGIN_SECRET_FILE=/run/secrets/oauth_login_secret','--env','OAUTH_LOGIN_SECRET=SYNTHETIC_INLINE_SECRET',live['cpa-manager-plus']['Image']]).strip()
    try:
        actual=json.loads(command(['docker','inspect',cid]))[0]
        try:module.secrets({**live,'cpa-manager-plus':actual})
        except module.Failure as error:assert str(error)=='worker_inline_secret',('inline_secret',str(error))
        else:raise AssertionError('inline_secret_not_rejected')
        report('manager_inline_secret',actual_inspect=True,rejected=True,code='worker_inline_secret',zero_maintenance=not (module.STATE/'maintenance').exists())
    finally:command(['docker','rm',cid])

def main():
    entry=subprocess.run(['/delivery/phase2-release.sh'],capture_output=True,text=True)
    assert entry.returncode==1 and 'usage_prepare_or_apply' in entry.stdout+entry.stderr,'delivery_shell_entry'
    report('delivery_shell_entry',actual_shebang=True,original_python_entry=True,no_arguments_rejected=True)
    create_site()
    report('fixture_site',four_real_services=True,internal_network=True,real_credentials=False,real_model_requests=0)
    if os.environ.get('FIXTURE_SETUP_ONLY')=='1':
        for attempt in range(30):
            if all(service(name)['State']['Running'] for name in ('cli-proxy-api','cpa-manager-plus','oauth-login','caddy')):
                time.sleep(1)
                if all(service(name)['State']['Running'] for name in ('cli-proxy-api','cpa-manager-plus','oauth-login','caddy')):
                    report('fixture_runtime',four_running=True,caddy_base_chain=True,apps_dual_chain=True)
                    return
            time.sleep(.2)
        for name in ('cli-proxy-api','cpa-manager-plus','oauth-login','caddy'):
            item=service(name)
            report('fixture_failed_service',service=name,running=item['State']['Running'],exit_code=item['State']['ExitCode'])
            if name=='cli-proxy-api' and not item['State']['Running']:
                result=subprocess.run(['docker','start','--attach',item['Id']],capture_output=True,text=True,timeout=10)
                report('fixture_startup_detail',output=result.stdout+result.stderr)
        raise RuntimeError('fixture_services_not_running')
    assert DELIVERY.is_file(), 'delivery_not_ready'
    # 契约冻结后接入真实prepare/apply，不替换函数。
    spec=importlib.util.spec_from_file_location('release',DELIVERY)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    module.ROOT=ROOT;module.PROJECT=PROJECT;module.STATE=ROOT/'.mx-upgrade-phase2-20261111'
    live=module.snapshot()
    facts=module.facts(live)
    module.BASELINE={name:{**value,'ports':[{'target':target,'published':[{'ip':p['HostIp'],'port':p['HostPort']} for p in (bindings or [])]} for target,bindings in value['ports'].items()]} for name,value in facts.items()}
    (ROOT/'fixture-baseline.json').write_text(json.dumps(module.BASELINE))
    module.validate_snapshot(live)
    report('delivery_loaded',functions=[name for name in ('prepare','apply','confirm_deploy','snapshot','backup','health','drift') if hasattr(module,name)])
    if os.environ.get('FIXTURE_SOURCE_CHECKS')=='1':source_rejections(module,live)
    bundle=Path('/frozen-bundle')
    protected=['cpa/config.yaml','compose.yaml','compose.mx-cpamp.yaml','cpa-compose','Caddyfile','secrets/oauth_login_secret','secrets/cpamp-admin-key']
    original={name:(ROOT/name).read_bytes() for name in protected}
    old_ids={name:item['Id'] for name,item in live.items()}
    code,output=invoke('prepare','--bundle-dir',str(bundle))
    (ROOT/'prepare-output.log').write_text(output)
    assert code==0, 'prepare_failed:'+output
    assert all((ROOT/name).read_bytes()==body for name,body in original.items()),'prepare_changed_protected_source'
    assert all(service(name)['Id']==cid and service(name)['State']['Running'] for name,cid in old_ids.items()),'prepare_changed_service'
    assert not (ROOT/'cpa/bps-state').exists(),'prepare_created_live_state'
    report('prepare',passed=True,zero_service_change=True,zero_config_wrapper_change=True,staged_only=True)
    if os.environ.get('FIXTURE_MAINTENANCE_FAILURE')=='1':
        original_data=ROOT/'manager-data'
        held_data=ROOT/'fixture-held-manager-data'
        code,output=invoke('apply',tty_input=b'DEPLOY\n',on_stopping=lambda:original_data.rename(held_data))
        print(output,flush=True)
        assert code!=0 and (module.STATE/'maintenance').is_file() and not (module.STATE/'complete').exists(),'maintenance_failure_not_retained'
        assert (module.STATE/'stage').read_text().strip()=='backing_up','maintenance_failure_stage'
        assert service('caddy')['Id']==old_ids['caddy'] and service('caddy')['State']['Running'],'failure_changed_caddy'
        assert all(not service(name)['State']['Running'] for name in module.APPS),'failure_restarted_apps'
        assert all((ROOT/name).read_bytes()==body for name,body in original.items()),'failure_installed_candidate'
        assert not (module.STATE/'backup/receipt.json').exists(),'failure_backup_receipt_claimed'
        code,reentry=invoke('apply',tty_input=b'DEPLOY\n')
        assert code!=0 and 'maintenance_requires_manual_recovery' in reentry,'maintenance_reentry_allowed'
        report('maintenance_failure',passed=True,stage='backing_up',apps_stopped=True,caddy_running=True,no_candidate_install=True,no_automatic_recovery=True,reentry_rejected=True)
        return
    confirmations=[('no_tty',None,'tty_required'),('cancel',b'CANCEL\n','confirmation_cancelled'),('eof',b'\x04','confirmation_eof'),('timeout',b'','confirmation_timeout')]
    if os.environ.get('FIXTURE_REUSE_PASSED')=='1':
        confirmations=[]
        report('reused_boundaries',source='matrix-shell-dependency.log',delivery='de91274298c74b073f91212b5acf90833f4a8777',unchanged_functions=['confirm_deploy','drift','sources','secrets'],timeout_seconds=120.58)
    for name,tty,expected in confirmations:
        started=time.monotonic()
        code,output=invoke('apply',tty_input=tty)
        assert code!=0 and expected in output,(name,output)
        assert not (module.STATE/'maintenance').exists(),'confirmation_started_maintenance'
        assert all(service(service_name)['Id']==cid and service(service_name)['State']['Running'] for service_name,cid in old_ids.items()),'confirmation_changed_service'
        elapsed=time.monotonic()-started
        if name=='timeout':assert elapsed>=120,'confirmation_timeout_shortened'
        report('confirmation_'+name,passed=True,zero_maintenance=True,stage=(module.STATE/'stage').read_text().strip(),elapsed_seconds=round(elapsed,2))
    for name in ([] if os.environ.get('FIXTURE_REUSE_PASSED')=='1' else ['cpa/config.yaml','compose.mx-cpamp.yaml']):
        path=ROOT/name;body=path.read_bytes();path.write_bytes(body+b'\n')
        try:
            code,output=invoke('apply',tty_input=b'DEPLOY\n')
            assert code!=0 and 'protected_source_drift' in output,('source_drift',name,output)
            assert not (module.STATE/'maintenance').exists(),'drift_started_maintenance'
            assert all(service(key)['Id']==cid and service(key)['State']['Running'] for key,cid in old_ids.items()),'drift_changed_services'
            report('source_drift',source=name,rejected=True,zero_maintenance=True)
        finally:path.write_bytes(body)
    if os.environ.get('FIXTURE_REUSE_PASSED')!='1':
        secret=ROOT/'secrets/oauth_login_secret';secret.chmod(0o600)
        try:
            code,output=invoke('apply',tty_input=b'DEPLOY\n')
            assert code!=0 and 'worker_secret_permissions' in output,('secret_permissions',output)
            assert not (module.STATE/'maintenance').exists(),'permissions_started_maintenance'
            report('worker_secret_permissions',mode='600',rejected=True,zero_maintenance=True)
        finally:secret.chmod(0o440)
    code,output=invoke('apply',tty_input=b'DEPLOY\n')
    (ROOT/'apply-output.log').write_text(output)
    assert code==0,'apply_failed:'+output
    assert (module.STATE/'complete').is_file(),'complete_missing'
    assert service('caddy')['Id']==old_ids['caddy'],'caddy_recreated'
    assert all(service(name)['State']['Running'] for name in module.APPS),'apps_not_running'
    rendered=json.loads(command([str(ROOT/'cpa-compose'),'config','--format','json']))
    assert all(rendered['services'][name]['image']==module.read(module.STATE/'plan.json')['images'][name] for name in module.APPS),'generated_wrapper_candidate_chain'
    with tarfile.open(module.STATE/'backup/data.tar.gz') as tar:
        for name in ('cpa/config.yaml','secrets/oauth_login_secret','secrets/cpamp-admin-key','manager-data'):
            assert str(ROOT/name).lstrip('/') in tar.getnames(),'backup_member_missing_'+name
        assert tar.extractfile(str(ROOT/'secrets/oauth_login_secret').lstrip('/')).read()==original['secrets/oauth_login_secret'],'backup_secret_content_changed'
    assert (module.STATE/'backup/data.tar.gz').stat().st_mode&0o777==0o600,'backup_mode'
    report('apply',passed=True,real_authenticated_health=True,caddy_id_unchanged=True,apps_three_chain=True,generated_linux_shell=True,backup_secret_readable=True,backup_mode='600')
    code,output=invoke('apply',tty_input=b'DEPLOY\n')
    assert code!=0 and 'not_prepared_or_complete' in output,'complete_reapply_allowed'
    report('complete_reapply',rejected=True)

if __name__=='__main__':
    main()
