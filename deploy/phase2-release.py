#!/usr/bin/env python3
"""固定四服务现场的二期发布；失败保留阶段，不自动回灌数据。"""
import copy
import fcntl
import gzip
import hashlib
import json
import os
import pathlib
import posixpath
import re
import select
import shutil
import subprocess
import sys
import tarfile
import time

ROOT = pathlib.Path('/opt/cpa-stack')
PROJECT = 'cpa-stack'
STATE = ROOT / '.mx-upgrade-phase2-20261111'
APPS = ['oauth-login', 'cpa-manager-plus', 'cli-proxy-api']
CANDIDATES = {
    'cli-proxy-api': {'role':'cpa','ref':'mx-cpa:bps-tool-diagnostic-20261111','revision':'ef4d287ea3298a7680f8fc21e82db77417bbe634','accepted_ids':['sha256:a4428de0eeda96d76cfa5733eed190ea8b45cd49083bf6b881f4bc9dfc1f5933','sha256:8a47d757034a09d391188113a5b72a745149f87da880a5313e2fb558b5d2512e']},
    'cpa-manager-plus': {'role':'manager','ref':'mx-cpamp:bps-tools-images-20261111','revision':'bd573d0439a925d811e25aeeb575ac91feb5796d','accepted_ids':['sha256:6732b8d85cfade68fce21ae286ede773d8151a397c64eedbea0b4618d4b6cd73','sha256:7c1e360f0ad697560eb1d4bcca60ee40782be6ff5d7b72dcc89a85ce225b3d8e']},
    'oauth-login': {'role':'worker','ref':'mx-oauth-login:phase2-local-20261111','revision':'95dd9ca2c16d5469c43ce7c910f38ff271af45e3','accepted_ids':['sha256:2bfdb42551061d6c0ba1b71758402ab46bc69a8843aa8f0273d6d634472c6758','sha256:30689442b2f61b2c7ff27eae008291b014fe7161e842a60202a6574c912be4dd']},
}
BASELINE = {'cli-proxy-api': {'kind': 'service', 'service': 'cli-proxy-api', 'id': '0cf979adbe916fba8892309800f7940a28bb6038f17428e67b8f2c4de1120a53', 'image_id': 'sha256:58d0177203d8f4e4968cebc60c59bdb0e236404a05c27656a38351dc1022ab93', 'image_ref': 'mx-cpa:20261110-rc1', 'revision': '5c90a92eef27f450c7080bc8735c88d873fc8915', 'status': 'running', 'health': 'unknown', 'network_mode': 'cpa-stack_default', 'networks': ['cpa-stack_default'], 'ports': [{'target': '8317/tcp', 'published': []}], 'mounts': [{'type': 'bind', 'source': '/opt/cpa-stack/cpa/config.yaml', 'target': '/CLIProxyAPI/config.yaml', 'rw': True}, {'type': 'bind', 'source': '/opt/cpa-stack/cpa/logs', 'target': '/CLIProxyAPI/logs', 'rw': True}, {'type': 'bind', 'source': '/opt/cpa-stack/cpa/plugins', 'target': '/CLIProxyAPI/plugins', 'rw': True}, {'type': 'bind', 'source': '/opt/cpa-stack/cpa/auth', 'target': '/root/.cli-proxy-api', 'rw': True}], 'working_dir': '/opt/cpa-stack', 'compose_files': ['/opt/cpa-stack/compose.yaml', '/opt/cpa-stack/compose.mx-cpamp.yaml']}, 'cpa-manager-plus': {'kind': 'service', 'service': 'cpa-manager-plus', 'id': 'ea0af0d5c8acff58abd9e96adb48687d24a4d03e381c2a123b1c4952931531eb', 'image_id': 'sha256:85a8f76852c08de220ba132490cbdf234b87f1bdeba91a6e0d9bfa905769a25c', 'image_ref': 'mx-cpamp:20261110-rc1', 'revision': '07572357832b69a30e3ee6c45f9f0cc2c3350e0c', 'status': 'running', 'health': 'healthy', 'network_mode': 'cpa-stack_default', 'networks': ['cpa-stack_default'], 'ports': [{'target': '18317/tcp', 'published': []}], 'mounts': [{'type': 'bind', 'source': '/opt/cpa-stack/manager-data', 'target': '/data', 'rw': True}, {'type': 'bind', 'source': '/opt/cpa-stack/secrets/cpamp-admin-key', 'target': '/run/secrets/cpamp-admin-key', 'rw': False}, {'type': 'bind', 'source': '/opt/cpa-stack/secrets/oauth_login_secret', 'target': '/run/secrets/oauth_login_secret', 'rw': False}], 'working_dir': '/opt/cpa-stack', 'compose_files': ['/opt/cpa-stack/compose.yaml', '/opt/cpa-stack/compose.mx-cpamp.yaml']}, 'oauth-login': {'kind': 'service', 'service': 'oauth-login', 'id': '880b9f091993a61152a732568447ba60026eb09c1bad6fdfb746c27ffbf81cce', 'image_id': 'sha256:64168881ce990f58d24736d1f8488549457ed2c775a072c1d0572bd524b7926b', 'image_ref': 'mx-oauth-login:20261110-rc1', 'revision': '42b04ac71df347e9c22fa35bad484c437da52d4b', 'status': 'running', 'health': 'unknown', 'network_mode': 'cpa-stack_default', 'networks': ['cpa-stack_default'], 'ports': [{'target': '18417/tcp', 'published': []}], 'mounts': [{'type': 'bind', 'source': '/opt/cpa-stack/secrets/oauth_login_secret', 'target': '/run/secrets/oauth_login_secret', 'rw': False}], 'working_dir': '/opt/cpa-stack', 'compose_files': ['/opt/cpa-stack/compose.yaml', '/opt/cpa-stack/compose.mx-cpamp.yaml']}, 'caddy': {'kind': 'service', 'service': 'caddy', 'id': '65e16a7220570b0847458b94eba1e779cff90a7cb50095e1dd00261fc41854ab', 'image_id': 'sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d', 'image_ref': 'caddy:2.10.2-alpine', 'revision': 'unknown', 'status': 'running', 'health': 'unknown', 'network_mode': 'cpa-stack_default', 'networks': ['cpa-stack_default'], 'ports': [{'target': '2019/tcp', 'published': []}, {'target': '443/tcp', 'published': [{'ip': '0.0.0.0', 'port': '443'}, {'ip': '::', 'port': '443'}]}, {'target': '443/udp', 'published': []}, {'target': '80/tcp', 'published': [{'ip': '0.0.0.0', 'port': '80'}, {'ip': '::', 'port': '80'}]}], 'mounts': [{'type': 'volume', 'source': '/var/lib/docker/volumes/cpa-stack_caddy_config/_data', 'target': '/config', 'rw': True}, {'type': 'volume', 'source': '/var/lib/docker/volumes/cpa-stack_caddy_data/_data', 'target': '/data', 'rw': True}, {'type': 'bind', 'source': '/opt/cpa-stack/Caddyfile', 'target': '/etc/caddy/Caddyfile', 'rw': False}], 'working_dir': '/opt/cpa-stack', 'compose_files': ['/opt/cpa-stack/compose.yaml']}}

class Failure(Exception): pass

def require(value, code):
    if not value: raise Failure(code)

def command(args, data=None):
    result = subprocess.run([str(x) for x in args], input=data, capture_output=True, text=True)
    require(result.returncode == 0, 'command_failed')
    return result.stdout

def read(path): return json.loads(pathlib.Path(path).read_text())

def write(path, value):
    path = pathlib.Path(path)
    raw = json.dumps(value, sort_keys=True, indent=2)+'\n'
    path.write_text(raw)
    path.chmod(0o600)

def digest(path): return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()

def mark(stage):
    require(re.fullmatch('[a-z_]+',stage), 'invalid_stage')
    (STATE/'stage').write_text(stage+'\n')
    print('stage='+stage, flush=True)

def compose(extra=False):
    files = [ROOT/'compose.yaml', ROOT/'compose.mx-cpamp.yaml']
    if extra: files.append(STATE/'compose.phase2.yaml')
    return ['docker','compose','--project-directory',str(ROOT),'-p',PROJECT]+sum((['-f',str(p)] for p in files),[])

def mounts(c):
    return sorted([{'type':m['Type'],'source':m['Source'],'target':m['Destination'],'rw':m['RW']} for m in c['Mounts']],key=lambda m:m['target'])

def snapshot():
    ids = command(['docker','ps','-aq','--filter','label=com.docker.compose.project='+PROJECT]).split()
    require(ids,'services_missing')
    live = json.loads(command(['docker','inspect']+ids))
    result = {}
    for c in live:
        labels = c['Config'].get('Labels') or {}
        if str(labels.get('com.docker.compose.oneoff','')).lower()=='true': continue
        service = labels.get('com.docker.compose.service')
        require(service in APPS+['caddy'] and service not in result,'service_set_changed')
        result[service] = c
    require(set(result)==set(APPS+['caddy']),'service_set_changed')
    return result

def facts(live):
    out = {}
    for service,c in live.items():
        labels=c['Config'].get('Labels') or {}
        out[service]={'id':c['Id'],'image_id':c['Image'],'image_ref':c['Config']['Image'],'mounts':mounts(c),'network_mode':c['HostConfig']['NetworkMode'],'networks':sorted(c['NetworkSettings']['Networks']),'ports':c['NetworkSettings']['Ports'],'working_dir':labels.get('com.docker.compose.project.working_dir'),'compose_files':labels.get('com.docker.compose.project.config_files','').split(','),'env':sorted(c['Config'].get('Env',[])),'args':c.get('Args',[])}
        out[service]['host_config']={k:v for k,v in c['HostConfig'].items() if k not in ['Binds','Mounts']}
        out[service]['process']={k:c['Config'].get(k) for k in ['User','WorkingDir','Entrypoint','Cmd']}
    return out

def compose_matches_live(rendered,live):
    require(set(rendered['services'])==set(live),'compose_services_changed')
    for service,c in live.items():
        planned=rendered['services'][service];hc=c['HostConfig'];cc=c['Config']
        require(planned.get('image')==cc['Image'],'compose_image_drift')
        image=json.loads(command(['docker','image','inspect',c['Image']]))[0]['Config']
        environment=dict(x.split('=',1) for x in image.get('Env',[]) if '=' in x)
        environment.update({k:str(v if v is not None else '') for k,v in planned.get('environment',{}).items()})
        require(environment==env(c),'compose_environment_drift')
        for key,field in [('command','Cmd'),('entrypoint','Entrypoint'),('user','User'),('working_dir','WorkingDir')]:
            expected=planned.get(key,image.get(field))
            default=[] if field in ['Cmd','Entrypoint'] else ''
            require((cc.get(field) or default)==(expected or default),'compose_process_drift')
        expected_mounts=[]
        for item in planned.get('volumes',[]):
            source=item.get('source');kind=item['type']
            if kind=='volume':
                definition=rendered.get('volumes',{}).get(source,{})
                name=definition.get('name',PROJECT+'_'+source)
                volume=json.loads(command(['docker','volume','inspect',name]))[0]
                source=volume['Mountpoint']
            require(kind in ['bind','volume'],'compose_mount_type_unknown')
            expected_mounts.append({'type':kind,'source':source,'target':item['target'],'rw':not item.get('read_only',False)})
        for item in planned.get('secrets',[]):
            item={'source':item} if isinstance(item,str) else item
            source=rendered.get('secrets',{}).get(item['source'],{}).get('file')
            require(source,'compose_secret_source_unknown')
            target=item.get('target',item['source'])
            target=target if target.startswith('/') else '/run/secrets/'+target
            expected_mounts.append({'type':'bind','source':source,'target':target,'rw':False})
        require(mounts(c)==sorted(expected_mounts,key=lambda x:x['target']),'compose_mount_drift')
        networks=planned.get('networks') or {'default':None}
        names=sorted(rendered.get('networks',{}).get(n,{}).get('name',PROJECT+'_'+n) for n in networks)
        require(not planned.get('network_mode') and sorted(c['NetworkSettings']['Networks'])==names and hc['NetworkMode'] in names,'compose_network_drift')
        bindings={}
        for p in planned.get('ports',[]):
            require(p.get('mode','ingress') in ['ingress','host'] and p.get('published'),'compose_port_unknown')
            bindings.setdefault(str(p['target'])+'/'+p.get('protocol','tcp'),[]).append({'HostIp':p.get('host_ip',''),'HostPort':str(p['published'])})
        normalize=lambda ports:{k:sorted(v or [],key=lambda x:(x['HostIp'],x['HostPort'])) for k,v in (ports or {}).items() if v}
        require(normalize(hc.get('PortBindings'))==normalize(bindings),'compose_port_drift')
        for key,field in [('init','Init'),('read_only','ReadonlyRootfs'),('privileged','Privileged')]:
            require(bool(planned.get(key,False))==bool(hc.get(field,False)),'compose_runtime_drift')
        for key,field in [('pid','PidMode'),('ipc','IpcMode'),('userns_mode','UsernsMode'),('uts','UTSMode')]:
            require(planned.get(key,'private' if key=='ipc' else '')==(hc.get(field) or ''),'compose_namespace_drift')
        for key,field in [('cap_add','CapAdd'),('cap_drop','CapDrop')]:
            canonical=lambda values:sorted(x.upper().removeprefix('CAP_') for x in values or [])
            require(canonical(planned.get(key))==canonical(hc.get(field)),'compose_capability_drift')
        require(sorted(planned.get('security_opt',[]))==sorted(hc.get('SecurityOpt') or []),'compose_security_drift')
        tmpfs={}
        for value in planned.get('tmpfs',[]):
            path,_,options=value.partition(':');tmpfs[path]=options
        require(tmpfs==(hc.get('Tmpfs') or {}),'compose_tmpfs_drift')
        restart=planned.get('restart','no').split(':',1)
        require(hc.get('RestartPolicy')=={'Name':restart[0],'MaximumRetryCount':int(restart[1]) if len(restart)>1 else 0},'compose_restart_drift')

def validate_snapshot(live):
    require(set(BASELINE)==set(APPS+['caddy']),'baseline_missing')
    actual=facts(live)
    for service,want in BASELINE.items():
        got=actual[service]
        require(live[service]['State']['Running'],'baseline_not_running')
        for field in ['id','image_id','image_ref','working_dir','compose_files','network_mode','networks','mounts']:
            expected=want[field]
            if field=='mounts': expected=sorted(expected,key=lambda m:m['target'])
            require(got[field]==expected,'baseline_'+field+'_changed')
        ports=sorted((port,b['HostIp'],b['HostPort']) for port,bindings in got['ports'].items() for b in (bindings or []))
        expected=sorted((p['target'],b['ip'],b['port']) for p in want['ports'] for b in p['published'])
        require(ports==expected,'baseline_ports_changed')
    return actual

def yaml_document(path):
    text=pathlib.Path(path).read_text()
    wrapper='services: {}\nx-document:\n'+''.join('  '+line+'\n' for line in text.splitlines())
    return json.loads(command(['docker','compose','--project-directory',ROOT,'-f','-','config','--no-interpolate','--format','json'],wrapper))['x-document']

def config():
    cfg=yaml_document(ROOT/'cpa/config.yaml')
    require(isinstance(cfg,dict) and cfg.get('config-version')==8,'config_version_unknown')
    return cfg

def env(c): return dict(x.split('=',1) for x in c['Config'].get('Env',[]) if '=' in x)

def resolve(path,cwd='/CLIProxyAPI'):
    path=str(path).strip()
    if path=='~' or path.startswith('~/'):path='/root/'+path[2:] if path!='~' else '/root'
    return posixpath.normpath(path if path.startswith('/') else posixpath.join(cwd,path))

def mapped(live,service,path):
    for m in sorted(live[service]['Mounts'],key=lambda x:-len(x['Destination'])):
        dest=m['Destination'].rstrip('/')
        if m['Type']=='bind' and (path==dest or path.startswith(dest+'/')):
            return pathlib.Path(m['Source']+path[len(dest):])
    raise Failure('path_not_persistent')

def cpa_config_source(live):
    c=live['cli-proxy-api'];variables=env(c);cwd=c['Config'].get('WorkingDir') or '/'
    require(variables.get('HOME','/root')=='/root','cpa_home_changed')
    for key in ['HOME_JWT','PGSTORE_DSN','GITSTORE_GIT_URL','OBJECTSTORE_ENDPOINT','WRITABLE_PATH']:
        require(not variables.get(key,'').strip() and not variables.get(key.lower(),'').strip(),'cpa_external_config_mode')
    args=list(c.get('Args',[]));selected='config.yaml'
    while args:
        arg=args.pop(0)
        if arg in ['-config','--config']:
            require(args,'cpa_config_argument_missing');selected=args.pop(0)
        elif arg.startswith(('-config=','--config=')):selected=arg.split('=',1)[1]
        else:raise Failure('cpa_arguments_unknown')
    require(mapped(live,'cli-proxy-api',resolve(selected,cwd))==ROOT/'cpa/config.yaml','cpa_config_source_changed')
    check='test ! -e "$1" && printf absent'
    require(command(['docker','exec',c['Id'],'sh','-c',check,'sh',posixpath.join(cwd,'.env')])=='absent','cpa_dotenv_source_unknown')
    return cwd

def manager_config(live):
    c=live['cpa-manager-plus'];selected=env(c).get('CPA_MANAGER_CONFIG','').strip()
    if not selected:return {},'/usr/local/bin',None
    selected=resolve(selected,c['Config'].get('WorkingDir') or '/')
    require(selected.startswith('/data/'),'manager_external_config')
    host=mapped(live,'cpa-manager-plus',selected)
    require(host.is_file(),'manager_config_missing')
    return read(host),posixpath.dirname(selected),host

def sources(live,cfg):
    cwd=cpa_config_source(live)
    auth=resolve(cfg.get('oauth',{}).get('auth-dir',cfg.get('auth-dir','/root/.cli-proxy-api')),cwd)
    require(mapped(live,'cli-proxy-api',auth)==ROOT/'cpa/auth','auth_path_changed')
    kv=cfg.get('server',{}).get('kv-store',cfg.get('kv-store',{})) or {}
    require(not kv.get('type') or kv.get('type') in ['bbolt','bolt'],'kv_backend_unknown')
    kv_path=mapped(live,'cli-proxy-api',resolve(kv.get('path') or '/root/.cli-proxy-api/kv.db',cwd))
    require(kv_path.is_relative_to(ROOT/'cpa/auth') or kv_path.is_relative_to(ROOT/'cpa/plugins'),'external_kv_not_covered')
    protocol=cfg.get('bps-protocol') or {}
    require(protocol.get('image-transport','native') in ['', 'native'] and not protocol.get('image-relay-base-url'),'relay_route_not_authorized')
    old_state=resolve(protocol.get('state-dir') or posixpath.join(posixpath.dirname(auth),'bps-state'),cwd)
    require(old_state!=auth and not old_state.startswith(auth+'/'),'state_inside_auth')
    check='if [ -e "$1" ]; then printf exists; elif [ -d "$(dirname -- "$1")" ] && [ -r "$(dirname -- "$1")" ]; then printf absent; else printf unknown; fi'
    require(command(['docker','exec',live['cli-proxy-api']['Id'],'sh','-c',check,'sh',old_state])=='absent','existing_or_unknown_bps_state')
    require(not (ROOT/'cpa/bps-state').exists(),'target_bps_state_exists')
    manager_env=env(live['cpa-manager-plus'])
    tracked=[ROOT/'compose.yaml',ROOT/'compose.mx-cpamp.yaml',ROOT/'cpa/config.yaml',ROOT/'Caddyfile',ROOT/'cpa-compose',ROOT/'secrets/cpamp-admin-key',ROOT/'secrets/oauth_login_secret']
    for key in ['USAGE_DATA_DIR','USAGE_DB_PATH','CPA_MANAGER_DATA_KEY_PATH','CPA_MANAGER_DATA_KEY_FILE','CPA_MANAGER_CONFIG']:
        if manager_env.get(key):
            p=resolve(manager_env[key],live['cpa-manager-plus']['Config'].get('WorkingDir') or '/')
            require(p=='/data' or p.startswith('/data/'),'manager_external_data_path')
    stored,config_dir,host=manager_config(live)
    if host:
        tracked.append(host)
        for key in ['dataDir','dbPath','dataKeyPath','dataKeyFile']:
            if stored.get(key):
                p=resolve(stored[key],config_dir)
                require(p=='/data' or p.startswith('/data/'),'manager_config_external_data')
    else:
        require(manager_env.get('USAGE_DATA_DIR') or manager_env.get('USAGE_DB_PATH'),'manager_data_source_unknown')
        require(command(['docker','exec',live['cpa-manager-plus']['Id'],'sh','-c','test ! -e /usr/local/bin/config.json && printf absent'])=='absent','manager_unmapped_config')
    require(not manager_env.get('CPA_MANAGER_DATA_KEY','').strip(),'manager_inline_data_key')
    data_dir=manager_env.get('USAGE_DATA_DIR') or resolve(stored.get('dataDir') or ('./data' if host else '/data'),config_dir)
    key_file=manager_env.get('CPA_MANAGER_DATA_KEY_FILE') or (resolve(stored['dataKeyFile'],config_dir) if stored.get('dataKeyFile') else '/run/secrets/cpa_data_key')
    if key_file=='/run/secrets/cpa_data_key':
        require(command(['docker','exec',live['cpa-manager-plus']['Id'],'sh','-c','test ! -e /run/secrets/cpa_data_key && printf absent'])=='absent','manager_unmapped_data_key')
        key_file=manager_env.get('CPA_MANAGER_DATA_KEY_PATH') or (resolve(stored['dataKeyPath'],config_dir) if stored.get('dataKeyPath') else posixpath.join(data_dir,'data.key'))
    key_file=resolve(key_file,live['cpa-manager-plus']['Config'].get('WorkingDir') or '/')
    data_key=mapped(live,'cpa-manager-plus',key_file)
    require(data_key.is_relative_to(ROOT/'manager-data') and data_key.is_file() and data_key.stat().st_size>0,'manager_data_key_missing')
    require(command(['docker','exec',live['cpa-manager-plus']['Id'],'sh','-c','test -r "$1" && test -s "$1" && printf readable','sh',key_file])=='readable','manager_data_key_unreadable')
    tracked.append(data_key)
    if (ROOT/'.env').exists():tracked.append(ROOT/'.env')
    for compose_file in [ROOT/'compose.yaml',ROOT/'compose.mx-cpamp.yaml']:
        for service in yaml_document(compose_file).get('services',{}).values():
            items=service.get('env_file',[])
            for item in [items] if isinstance(items,str) else items:
                value=item['path'] if isinstance(item,dict) else item
                require('$' not in value,'environment_source_unknown')
                p=pathlib.Path(value)
                if not p.is_absolute():p=ROOT/p
                require(p.is_file(),'environment_source_unknown');tracked.append(p)
    require(all(p.is_file() for p in tracked),'protected_source_missing')
    return {'auth':auth,'kv_path':str(kv_path),'old_bps_state':old_state,'tracked':sorted(set(str(p) for p in tracked))}

def secrets(live):
    path=ROOT/'secrets/oauth_login_secret'
    s=path.stat()
    require((s.st_uid,s.st_gid,s.st_mode&0o777)==(0,1000,0o440),'worker_secret_permissions')
    require(len(path.read_bytes().strip())>=32,'worker_secret_invalid')
    for service in ['oauth-login','cpa-manager-plus']:
        require(mapped(live,service,'/run/secrets/oauth_login_secret')==path,'worker_secret_source_changed')
        require(env(live[service]).get('OAUTH_LOGIN_SECRET_FILE')=='/run/secrets/oauth_login_secret','worker_secret_selection_changed')
        require(not env(live[service]).get('OAUTH_LOGIN_SECRET','').strip(),'worker_inline_secret')
    code="const f=require('fs'),c=require('crypto');try{const b=f.readFileSync(process.env.OAUTH_LOGIN_SECRET_FILE);process.stdout.write(c.createHash('sha256').update(b).digest('hex'))}catch{process.exitCode=1}"
    require(command(['docker','exec',live['oauth-login']['Id'],'node','-e',code])==digest(path),'worker_secret_unreadable_or_different')
    require(mapped(live,'cpa-manager-plus','/run/secrets/cpamp-admin-key')==ROOT/'secrets/cpamp-admin-key','admin_source_changed')
    variables=env(live['cpa-manager-plus']);stored,config_dir,_=manager_config(live)
    require(not variables.get('CPA_MANAGER_ADMIN_KEY','').strip(),'manager_inline_admin_key')
    selected=variables.get('CPA_MANAGER_ADMIN_KEY_FILE') or (resolve(stored['adminKeyFile'],config_dir) if stored.get('adminKeyFile') else '/run/secrets/cpa_admin_key')
    require(selected=='/run/secrets/cpamp-admin-key','admin_selection_changed')

def image_records():
    result={}
    for service,expected in CANDIDATES.items():
        image=json.loads(command(['docker','image','inspect',expected['ref']]))[0]
        require(image['Id'] in expected['accepted_ids'],'candidate_image_id')
        require(image['Os']=='linux' and image['Architecture']=='amd64','candidate_platform')
        require((image['Config'].get('Labels') or {}).get('org.opencontainers.image.revision')==expected['revision'],'candidate_revision')
        result[service]=image['Id']
    return result

def load_bundle(source):
    mode,location=source
    bundle=STATE/'bundle';bundle.mkdir(mode=0o700)
    names=['mx-cpamp-images.manifest.json','mx-cpamp-images-linux-amd64.tar.gz']
    for name in names:
        if mode=='--bundle-dir':shutil.copyfile(pathlib.Path(location)/name,bundle/name)
        else:
            require(location.startswith('https://'),'release_url_requires_https')
            command(['curl','--fail','--silent','--show-error','--location','--proto','=https','--proto-redir','=https',location.rstrip('/')+'/'+name,'--output',bundle/name])
        (bundle/name).chmod(0o600)
    manifest=read(bundle/names[0])
    require(manifest.get('schema')==1 and manifest.get('archive',{}).get('file')==names[1],'manifest_schema')
    for expected in CANDIDATES.values():
        item=manifest.get('images',{}).get(expected['role'],{})
        require(all(item.get(k)==expected[k] for k in ['ref','revision','accepted_ids']) and item.get('os')=='linux' and item.get('architecture')=='amd64','manifest_candidate')
    require(digest(bundle/names[1])==manifest['archive'].get('sha256'),'archive_digest')
    with gzip.open(bundle/names[1],'rb') as stream:
        process=subprocess.Popen(['docker','load'],stdin=subprocess.PIPE,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        try:shutil.copyfileobj(stream,process.stdin)
        finally:process.stdin.close()
        require(process.wait()==0,'image_load_failed')
    return image_records()

def health(candidate=False):
    plan=read(STATE/'plan.json');live=snapshot()
    if candidate:
        for service in APPS:
            c=live[service]
            require(c['State']['Running'] and c['Image']==plan['images'][service],'candidate_runtime_identity')
            expected=copy.deepcopy(plan['facts'][service]['mounts'])
            if service=='cli-proxy-api':expected.append({'type':'bind','source':str(ROOT/'cpa/bps-state'),'target':'/var/lib/cpa/bps-state','rw':True})
            require(mounts(c)==sorted(expected,key=lambda x:x['target']),'candidate_runtime_mounts')
            labels=c['Config'].get('Labels') or {}
            require(labels.get('com.docker.compose.project.config_files')==','.join(str(p) for p in [ROOT/'compose.yaml',ROOT/'compose.mx-cpamp.yaml',STATE/'compose.phase2.yaml']),'candidate_runtime_chain')
        compose_matches_live(plan['rendered'],live)
    require(live['caddy']['Id']==plan['facts']['caddy']['id'] and live['caddy']['State']['Running'],'caddy_changed')
    cfg=config();keys=cfg.get('access',{}).get('api-keys',cfg.get('api-keys',[]))
    require(isinstance(keys,list) and keys and isinstance(keys[0],str),'api_key_unavailable')
    payload={'api':keys[0],'admin':(ROOT/'secrets/cpamp-admin-key').read_text().strip()}
    program="const fs=require('fs');let s='';process.stdin.on('data',c=>s+=c);process.stdin.on('end',async()=>{try{const k=JSON.parse(s),w=fs.readFileSync('/run/secrets/oauth_login_secret','utf8').trim();for(const [u,t,ok] of [['http://cli-proxy-api:8317/v1/models',k.api,200],['http://cpa-manager-plus:18317/api/oauth-login/tasks/00000000-0000-4000-8000-000000000001',k.admin,404],['http://oauth-login:18417/health',w,200]]){for(const [key,status] of [[t,ok],['invalid-phase2-probe',401]]){const r=await fetch(u,{redirect:'error',headers:{authorization:'Bearer '+key},signal:AbortSignal.timeout(5000)});if(r.status!==status)throw Error('probe');await r.arrayBuffer()}}process.stdout.write('ok')}catch{process.exitCode=1}})"
    require(command(['docker','exec','-i',live['oauth-login']['Id'],'node','-e',program],json.dumps(payload))=='ok','authenticated_health_failed')

def prepare(source):
    require(not (STATE/'ready').exists() and not (STATE/'maintenance').exists() and not (STATE/'complete').exists(),'state_already_used')
    require(not (STATE/'bundle').exists(),'prepare_already_attempted')
    mark('prepare_bundle');images=load_bundle(source)
    mark('prepare_site');live=snapshot();observed=validate_snapshot(live);cfg=config();origin=sources(live,cfg);secrets(live)
    require((ROOT/'.mx-upgrade-20261110-rc1/complete').is_file(),'rc1_complete_missing')
    merged=json.loads(command(compose()+['config','--format','json']))
    compose_matches_live(merged,live)
    candidate=copy.deepcopy(cfg)
    candidate.setdefault('bps-protocol',{})['state-dir']='/var/lib/cpa/bps-state'
    write(STATE/'cpa-config.yaml',candidate)
    override={'services':{s:{'image':images[s],'pull_policy':'never'} for s in APPS}}
    override['services']['cli-proxy-api']['volumes']=[{'type':'bind','source':str(ROOT/'cpa/bps-state'),'target':'/var/lib/cpa/bps-state'}]
    write(STATE/'compose.phase2.yaml',override)
    rendered=json.loads(command(compose(True)+['config','--format','json']))
    expected=copy.deepcopy(merged)
    for s in APPS:expected['services'][s].update(image=images[s],pull_policy='never')
    expected['services']['cli-proxy-api'].setdefault('volumes',[]).append({'type':'bind','source':str(ROOT/'cpa/bps-state'),'target':'/var/lib/cpa/bps-state','bind':{'create_host_path':True}})
    for value in [expected,rendered]:
        for service in value['services'].values():service['volumes']=sorted(service.get('volumes',[]),key=lambda x:x['target'])
    require(rendered==expected,'candidate_merge_changed')
    tracked=origin['tracked']+[str(pathlib.Path(__file__).resolve()),str(pathlib.Path(__file__).with_suffix('.sh').resolve())]
    plan={'facts':observed,'images':images,'sources':origin,'base':merged,'rendered':rendered,'digests':{p:digest(p) for p in tracked},'candidate_digests':{p:digest(STATE/p) for p in ['cpa-config.yaml','compose.phase2.yaml']}}
    write(STATE/'plan.json',plan)
    (STATE/'RECOVERY.txt').write_text('二期发布失败处理\n保留本目录、stage、plan.json、backup、compose.phase2.yaml 和新建 cpa/bps-state；不要删除 maintenance 或重复 apply。\n维护开始前失败：旧容器和原配置未切换，按错误类别修复后定向核证，不改旧 rc1 材料。\n维护开始后失败：先只读核三个应用实际状态、Caddy 原容器 ID、当前配置和三文件链；由维护者绑定本次 plan 和停态备份制定定向恢复。不要自动整库回灌；新凭证、运行时记录和切换后的数据必须先保留。\n新 wrapper 依赖本目录 compose.phase2.yaml，完成后也保留本目录。备份只作为经检查的恢复来源，不代表已恢复或业务验收。\n')
    health(False)
    (STATE/'ready').touch(mode=0o600);mark('prepared')
    print('prepared='+str(STATE)+' maintenance_started=no',flush=True)

def confirm_deploy():
    require(sys.stdin.isatty(),'tty_required')
    print('将短暂停止 Worker、Manager、CPA，可能中断现有登录和模型请求；Caddy 保留。输入 DEPLOY：',end='',flush=True)
    require(select.select([sys.stdin],[],[],120)[0],'confirmation_timeout')
    answer=sys.stdin.readline()
    require(answer!='','confirmation_eof')
    require(answer.rstrip('\r\n')=='DEPLOY','confirmation_cancelled')

def drift():
    plan=read(STATE/'plan.json');live=snapshot()
    require(validate_snapshot(live)==plan['facts'],'live_drift')
    for path,expected in plan['digests'].items():require(digest(path)==expected,'protected_source_drift')
    for path,expected in plan['candidate_digests'].items():require(digest(STATE/path)==expected,'candidate_material_drift')
    require(image_records()==plan['images'],'candidate_image_drift')
    require(json.loads(command(compose()+['config','--format','json']))==plan['base'],'compose_drift')
    compose_matches_live(plan['base'],live)
    require(sources(live,config())==plan['sources'],'state_or_data_source_drift')
    secrets(live);health(False)

def stopped():
    plan=read(STATE/'plan.json')
    for service,old in plan['facts'].items():
        c=json.loads(command(['docker','inspect',old['id']]))[0]
        require(c['State']['Running']==(service=='caddy'),'stop_not_confirmed')

def backup():
    stopped();plan=read(STATE/'plan.json');target=STATE/'backup'
    require(not target.exists(),'backup_exists')
    target.mkdir(mode=0o700)
    paths=[ROOT/'manager-data',ROOT/'cpa/auth']+[pathlib.Path(p) for p in plan['sources']['tracked']]
    kv=pathlib.Path(plan['sources']['kv_path'])
    if kv.exists() and not kv.is_relative_to(ROOT/'cpa/auth'):paths.append(kv)
    output=target/'data.tar.gz'
    with tarfile.open(output,'w:gz') as archive:
        for path in dict.fromkeys(paths):archive.add(path,arcname=str(path).lstrip('/'))
    output.chmod(0o600)
    with tarfile.open(output,'r:gz') as archive:
        names=archive.getnames()
        for path in paths:require(str(path).lstrip('/') in names,'backup_member_missing')
        for member in archive:
            if member.isfile():
                stream=archive.extractfile(member)
                while stream.read(1024*1024):pass
    write(target/'receipt.json',{'paths':[str(p) for p in paths],'bytes':output.stat().st_size,'sha256':digest(output),'stopped':True})

def install_candidate():
    stopped();require((STATE/'backup/receipt.json').is_file(),'backup_not_confirmed')
    require(not (ROOT/'cpa/bps-state').exists(),'target_state_exists')
    (ROOT/'cpa/bps-state').mkdir(mode=0o700)
    # 仅替换已停机的 CPA 配置，原始内容保留在私有备份。
    shutil.copyfile(STATE/'cpa-config.yaml',ROOT/'cpa/config.phase2-new')
    (ROOT/'cpa/config.phase2-new').chmod(0o600)
    os.replace(ROOT/'cpa/config.phase2-new',ROOT/'cpa/config.yaml')
    wrapper='#!/usr/bin/env bash\nset -eu\nexec docker compose --project-directory '+str(ROOT)+' -p '+PROJECT+' -f '+str(ROOT/'compose.yaml')+' -f '+str(ROOT/'compose.mx-cpamp.yaml')+' -f '+str(STATE/'compose.phase2.yaml')+' "$@"\n'
    (STATE/'cpa-compose').write_text(wrapper);(STATE/'cpa-compose').chmod(0o700)
    shutil.copyfile(STATE/'cpa-compose',ROOT/'cpa-compose.phase2-new');(ROOT/'cpa-compose.phase2-new').chmod(0o700)
    os.replace(ROOT/'cpa-compose.phase2-new',ROOT/'cpa-compose')

def apply():
    require((STATE/'ready').is_file() and not (STATE/'complete').exists(),'not_prepared_or_complete')
    require(not (STATE/'maintenance').exists(),'maintenance_requires_manual_recovery')
    require((STATE/'stage').read_text().strip() in ['prepared','awaiting_confirmation','pre_switch_drift'],'invalid_apply_stage')
    mark('awaiting_confirmation');confirm_deploy()
    mark('pre_switch_drift');drift()
    (STATE/'maintenance').touch(mode=0o600);mark('stopping_apps')
    for service in APPS:command(compose()+['stop','-t','30',service])
    stopped();mark('backing_up');backup()
    mark('publishing_candidate');install_candidate()
    mark('starting_candidates');command(compose(True)+['up','-d','--no-deps','--no-build','--pull','never']+APPS)
    mark('authenticated_health')
    for attempt in range(30):
        try:health(True);break
        except Failure:
            if attempt==29:raise
            time.sleep(2)
    (STATE/'complete').touch(mode=0o600);mark('complete')
    print('release_complete state='+str(STATE)+' backup='+str(STATE/'backup'),flush=True)

def main(argv=None):
    args=sys.argv[1:] if argv is None else argv
    os.umask(0o077)
    require(os.geteuid()==0,'root_required')
    require(args and (args==['apply'] or len(args)==3 and args[0]=='prepare' and args[1] in ['--bundle-dir','--release-url']),'usage_prepare_or_apply')
    require(ROOT.is_dir(),'stack_missing')
    STATE.mkdir(mode=0o700,exist_ok=True)
    require(STATE.stat().st_uid==0 and STATE.stat().st_mode&0o777==0o700,'state_permissions')
    with (STATE/'operation.lock').open('a') as lock:
        try:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        except OSError:raise Failure('operation_in_progress')
        if args[0]=='prepare':prepare(args[1:])
        else:apply()

if __name__=='__main__':
    try:main()
    except Exception as error:
        stage=(STATE/'stage').read_text().strip() if (STATE/'stage').is_file() else 'initial'
        stage=stage if re.fullmatch('[a-z_]+',stage) else 'unknown'
        code=str(error) if isinstance(error,Failure) and re.fullmatch('[a-z_]+',str(error)) else 'operation_failed'
        print('release_failed stage='+stage+' code='+code, file=sys.stderr)
        sys.exit(1)
