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
DELIVERY=Path('/delivery/bps-final-release.py')

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
    config['bps-protocol']={'enabled':False,'images-enabled':True,'image-transport':'relay','image-relay-base-url':'https://cpa.maixun.cyou/','state-dir':'/stale-state'}
    config['upstream']={'bps':{'protocol':{'enabled':None,'images-enabled':False,'image-transport':None,'image-relay-base-url':None,'state-dir':None}}}
    (ROOT/'cpa/config.yaml').write_text(json.dumps(config))
    manager={'httpAddr':'0.0.0.0:18317','dataDir':'/data','cpaUpstreamUrl':'http://cli-proxy-api:8317','managementKeyFile':'/data/cpa-key','adminKeyFile':'/run/secrets/cpamp-admin-key'}
    (ROOT/'manager-data/config.json').write_text(json.dumps(manager));(ROOT/'manager-data/cpa-key').write_text(management)
    # 真实Manager初始化自己的SQLite/WAL；测试不伪造数据库文件。
    (ROOT/'secrets/caddy-test-ca').write_text('')
    (ROOT/'Caddyfile').write_text('{\n    auto_https disable_redirects\n}\ncpamp.maixun.cyou {\n    tls internal\n    reverse_proxy cpa-manager-plus:18317\n}\ncpa.maixun.cyou {\n    tls internal\n    @api path /v1/* /v1beta/*\n    handle @api {\n        reverse_proxy cli-proxy-api:8317\n    }\n    handle {\n        respond 404\n    }\n}\n')
    (ROOT/'cpa-compose').write_text('#!/bin/sh\nexec docker compose --project-directory '+str(ROOT)+' -p '+PROJECT+' -f '+str(ROOT/'compose.yaml')+' -f '+str(ROOT/'compose.mx-cpamp.yaml')+' "$@"\n')
    (ROOT/'cpa-compose').chmod(0o700)
    common={'logging':{'driver':'none'},'labels':{'mx.bpsrelayboundary':PROJECT},'pull_policy':'never'}
    def bind(source,target,ro=False):return {'type':'bind','source':str(ROOT/source),'target':target,'read_only':ro}
    services={
      'cli-proxy-api':{**common,'image':'mx-cpa:20261110-rc1','command':['./CLIProxyAPI','--config','/CLIProxyAPI/config.yaml'],'volumes':[bind('cpa/config.yaml','/CLIProxyAPI/config.yaml'),bind('cpa/logs','/CLIProxyAPI/logs'),bind('cpa/plugins','/CLIProxyAPI/plugins'),bind('cpa/auth','/root/.cli-proxy-api')]},
      'cpa-manager-plus':{**common,'image':'mx-cpamp:20261110-rc1','environment':{'CPA_MANAGER_CONFIG':'/data/config.json'},'volumes':[bind('manager-data','/data'),bind('secrets/cpamp-admin-key','/run/secrets/cpamp-admin-key',True)]},
      'caddy':{**common,'image':'caddy:2.10.2-alpine','volumes':[bind('Caddyfile','/etc/caddy/Caddyfile',True),{'type':'volume','source':'caddy_data','target':'/data'},{'type':'volume','source':'caddy_config','target':'/config'}]}}
    existing=json.loads(command(['docker','network','inspect']+command(['docker','network','ls','-q']).split()))
    occupied=[ipaddress.ip_network(item['Subnet']) for network in existing for item in network.get('IPAM',{}).get('Config') or [] if item.get('Subnet')]
    subnet=next(ipaddress.ip_network('10.253.'+str(index)+'.0/28') for index in range(1,255) if not any(ipaddress.ip_network('10.253.'+str(index)+'.0/28').overlaps(old) for old in occupied if old.version==4))
    base={'services':services,'networks':{'default':{'internal':True,'ipam':{'config':[{'subnet':str(subnet)}]}}},'volumes':{'caddy_data':{},'caddy_config':{}}}
    override={'services':{'cpa-manager-plus':{'environment':{'OAUTH_LOGIN_URL':'http://oauth-login:18417','OAUTH_LOGIN_SECRET_FILE':'/run/secrets/oauth_login_secret'},'volumes':[bind('secrets/oauth_login_secret','/run/secrets/oauth_login_secret',True)]},'oauth-login':{**common,'image':'mx-oauth-login:20261110-rc1','environment':{'OAUTH_LOGIN_SECRET_FILE':'/run/secrets/oauth_login_secret','NODE_EXTRA_CA_CERTS':'/run/secrets/caddy-test-ca'},'read_only':True,'tmpfs':['/tmp:mode=1777,size=32m'],'volumes':[bind('secrets/oauth_login_secret','/run/secrets/oauth_login_secret',True),bind('secrets/caddy-test-ca','/run/secrets/caddy-test-ca',True)]}}}
    (ROOT/'compose.yaml').write_text(json.dumps(base));(ROOT/'compose.mx-cpamp.yaml').write_text(json.dumps(override))
    legacy=ROOT/'.mx-upgrade-20261110-rc1';legacy.mkdir(mode=0o700)
    (legacy/'complete').write_text('');(legacy/'backup').mkdir(mode=0o700)
    (legacy/'backup/historical').write_text('SYNTHETIC_OLD_BACKUP_MARKER')
    compose([ROOT/'compose.yaml'],'up','-d','--pull','never','--no-build')
    compose([ROOT/'compose.yaml',ROOT/'compose.mx-cpamp.yaml'],'up','-d','--pull','never','--no-build','--no-deps','cli-proxy-api','cpa-manager-plus','oauth-login')
    for attempt in range(50):
        cert=subprocess.run(['docker','exec',service('caddy')['Id'],'cat','/data/caddy/pki/authorities/local/root.crt'],capture_output=True,text=True)
        if cert.returncode==0:
            (ROOT/'secrets/caddy-test-ca').write_text(cert.stdout);break
        time.sleep(.2)
    else:raise RuntimeError('fixture_ca_missing')
    return config


def main():
    entry=subprocess.run(['/delivery/bps-final-release.sh'],capture_output=True,text=True)
    assert entry.returncode==1 and 'usage_prepare_or_apply' in entry.stdout+entry.stderr,'delivery_shell_entry'
    report('delivery_shell_entry',actual_shebang=True,no_arguments_rejected=True)
    create_site()
    report('fixture_site',four_real_services=True,internal_network=True,real_credentials=False,real_model_requests=0)
    spec=importlib.util.spec_from_file_location('release',DELIVERY)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    module.ROOT=ROOT;module.PROJECT=PROJECT;module.STATE=ROOT/'.mx-upgrade-bps-relay-20261111'
    live=module.snapshot()
    facts=module.facts(live)
    module.BASELINE={name:{**value,'ports':[{'target':target,'published':[{'ip':p['HostIp'],'port':p['HostPort']} for p in (bindings or [])]} for target,bindings in value['ports'].items()]} for name,value in facts.items()}
    (ROOT/'fixture-baseline.json').write_text(json.dumps(module.BASELINE))
    module.validate_snapshot(live)
    protected=['cpa/config.yaml','compose.yaml','compose.mx-cpamp.yaml','cpa-compose','Caddyfile','secrets/oauth_login_secret','secrets/cpamp-admin-key']
    original={name:(ROOT/name).read_bytes() for name in protected}
    old_ids={name:item['Id'] for name,item in live.items()}
    caddy_before=module.caddy_source(live)
    caddy_inode=(ROOT/'Caddyfile').stat().st_ino
    caddy_mounts=module.mounts(live['caddy'])
    code,output=invoke('prepare','--bundle-dir','/frozen-bundle')
    print(output,flush=True)
    assert code==0,'prepare_failed:'+output
    assert all((ROOT/name).read_bytes()==body for name,body in original.items()),'prepare_changed_protected_source'
    assert all(service(name)['Id']==cid and service(name)['State']['Running'] for name,cid in old_ids.items()),'prepare_changed_service'
    assert not (ROOT/'cpa/bps-state').exists(),'prepare_created_live_state'
    assert module.caddy_source(module.snapshot())==caddy_before,'prepare_reloaded_caddy'
    report('prepare',passed=True,zero_service_change=True,zero_config_wrapper_change=True,live_state_absent=True,caddy_runtime_unchanged=True)
    code,output=invoke('apply',tty_input=b'CANCEL\n')
    assert code!=0 and 'confirmation_cancelled' in output,'cancel_not_rejected:'+output
    assert not (module.STATE/'maintenance').exists(),'cancel_started_maintenance'
    assert all((ROOT/name).read_bytes()==body for name,body in original.items()),'cancel_changed_protected_source'
    assert all(service(name)['Id']==cid and service(name)['State']['Running'] for name,cid in old_ids.items()),'cancel_changed_service'
    report('confirmation_cancel',passed=True,real_pty=True,zero_maintenance=True,zero_service_change=True)
    if os.environ.get('FIXTURE_CADDY_FAILURE')=='1':
        code,output=invoke('apply',tty_input=b'DEPLOY\n',on_stopping=lambda:(module.STATE/'Caddyfile').write_text('}\n'))
        print(output,flush=True)
        assert code!=0 and (module.STATE/'stage').read_text().strip()=='reloading_caddy','reload_failure_stage'
        assert (module.STATE/'maintenance').exists() and not (module.STATE/'complete').exists(),'reload_failure_lost_maintenance'
        assert module.caddy_semantic(module.caddy_runtime(old_ids['caddy']))==module.caddy_semantic(caddy_before['adapted']),'reload_failure_runtime_not_old'
        assert (ROOT/'Caddyfile').read_text()=='}\n' and (ROOT/'Caddyfile').stat().st_ino==caddy_inode,'reload_failure_disk_not_new'
        assert service('caddy')['Id']==old_ids['caddy'] and service('caddy')['State']['Running'],'reload_failure_changed_caddy'
        assert all(service(name)['State']['Running'] for name in module.APPS),'reload_failure_recovered_apps'
        with tarfile.open(module.STATE/'backup/data.tar.gz') as tar:
            assert tar.extractfile(str(ROOT/'Caddyfile').lstrip('/')).read()==original['Caddyfile'],'reload_failure_backup_missing'
        code,reentry=invoke('apply',tty_input=b'DEPLOY\n')
        assert code!=0 and 'maintenance_requires_manual_recovery' in reentry,'reload_failure_reentry_allowed'
        report('caddy_reload_failure',passed=True,stage='reloading_caddy',disk_candidate_runtime_old=True,original_inode=True,old_caddy_running=True,backup_original_readable=True,maintenance_retained=True,reentry_rejected=True)
        return
    code,output=invoke('apply',tty_input=b'DEPLOY\n')
    print(output,flush=True)
    assert code==0,'apply_failed:'+output
    assert (module.STATE/'complete').is_file(),'complete_missing'
    assert service('caddy')['Id']==old_ids['caddy'] and service('caddy')['State']['Running'],'caddy_changed'
    assert all(service(name)['State']['Running'] for name in module.APPS),'apps_not_running'
    assert (ROOT/'cpa/bps-state').is_dir() and (ROOT/'cpa/bps-state').stat().st_mode&0o777==0o700,'live_state_mode'
    rendered=json.loads(command([str(ROOT/'cpa-compose'),'config','--format','json']))
    plan=module.read(module.STATE/'plan.json')
    assert all(rendered['services'][name]['image']==plan['images'][name] for name in module.APPS),'generated_wrapper_candidate_chain'
    actual_config=json.loads((ROOT/'cpa/config.yaml').read_text())
    assert module.effective_protocol(actual_config)['state-dir']=='/var/lib/cpa/bps-state','configured_state_path'
    assert not any(key in actual_config.get('bps-protocol',{}) for key in ['state-dir','image-transport','image-relay-base-url']),'legacy_touched_fields_retained'
    assert (ROOT/'Caddyfile').stat().st_ino==caddy_inode and module.mounts(service('caddy'))==caddy_mounts,'caddy_inode_or_volumes_changed'
    report('caddy_reload',passed=True,original_id=True,original_inode=True,certificate_volumes_unchanged=True,mounted_candidate_bytes=True,runtime_candidate=True,validated_internal_https_routes=True,settings_runtime_readback=True)
    with tarfile.open(module.STATE/'backup/data.tar.gz') as tar:
        for name in ('cpa/config.yaml','secrets/oauth_login_secret','secrets/cpamp-admin-key','manager-data'):
            assert str(ROOT/name).lstrip('/') in tar.getnames(),'backup_member_missing_'+name
        assert tar.extractfile(str(ROOT/'secrets/oauth_login_secret').lstrip('/')).read()==original['secrets/oauth_login_secret'],'backup_secret_content_changed'
    assert (module.STATE/'backup/data.tar.gz').stat().st_mode&0o777==0o600,'backup_mode'
    report('apply',passed=True,real_authenticated_health=True,caddy_id_unchanged=True,apps_three_chain=True,generated_linux_shell=True,backup_secret_readable=True,backup_mode='600',live_state_mode='700',images=plan['images'])
    code,output=invoke('apply',tty_input=b'DEPLOY\n')
    assert code!=0 and 'not_prepared_or_complete' in output,'complete_reapply_allowed'
    report('complete_reapply',rejected=True)


if __name__=='__main__':
    main()
