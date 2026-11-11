#!/usr/bin/env bash
# 只读盘点；Python、Compose 输出与认证材料仅留在进程内。
set -eu
command -v python3 >/dev/null 2>&1 || { printf '{"kind":"error","code":"python3_missing"}\n'; exit 1; }
exec python3 - "$@" <<'PY'
import ipaddress, json, os, pathlib, re, shutil, stat, subprocess, sys, urllib.request, urllib.error

def emit(kind, **fields):
    print(json.dumps(dict(kind=kind, **fields), ensure_ascii=True, separators=(',', ':')), flush=True)

def run(args, data=None):
    try:
        p = subprocess.run(args, input=data, text=True, capture_output=True, timeout=20)
        return p.stdout if p.returncode == 0 else None
    except Exception:
        return None

def obj(raw):
    try: return json.loads(raw)
    except Exception: return None

def safe(value, pattern=r'[A-Za-z0-9_./:@+ -]{1,512}'):
    return value if isinstance(value, str) and re.fullmatch(pattern, value) else 'unknown'

def path_info(path):
    try:
        p = pathlib.Path(path)
        s = p.stat()
        return dict(exists=True, type='directory' if stat.S_ISDIR(s.st_mode) else 'file' if stat.S_ISREG(s.st_mode) else 'other', mode=oct(stat.S_IMODE(s.st_mode))[2:], uid=s.st_uid, gid=s.st_gid, bytes=s.st_size)
    except FileNotFoundError: return dict(exists=False)
    except Exception: return dict(exists='unknown')

def read(path):
    try:
        with open(path, 'r') as f: return f.read(2*1024*1024)
    except Exception: return None

def host_path(container, target):
    for m in sorted(container.get('Mounts', []), key=lambda x: -len(x.get('Destination', ''))):
        dest = m.get('Destination', '').rstrip('/')
        if m.get('Type') == 'bind' and (target == dest or target.startswith(dest+'/')):
            return m['Source'] + target[len(dest):]
    return None

def yaml_doc(text, cwd):
    if text is None: return None
    wrapper = 'services: {}\nx-preflight:\n' + ''.join('  '+line+'\n' for line in text.splitlines())
    parsed = obj(run(['docker', 'compose', '--project-directory', cwd, '-f', '-', 'config', '--no-interpolate', '--format', 'json'], wrapper))
    return parsed.get('x-preflight') if isinstance(parsed, dict) else None

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs): return None

def get(address, port, path, key):
    if not address or not key: return None, None
    try:
        ip = ipaddress.ip_address(address)
        if not ip.is_private: return None, None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        req = urllib.request.Request('http://'+str(ip)+':'+str(port)+path, headers={'Authorization': 'Bearer '+key})
        try:
            with opener.open(req, timeout=5) as response:
                body=response.read(2*1024*1024+1)
                try: parsed=json.loads(body)
                except Exception: parsed={'_invalid_body':True}
                return response.status, parsed
        except urllib.error.HTTPError as error: return error.code, None
    except Exception: pass
    return None, None

def main():
    project = sys.argv[1] if len(sys.argv) > 1 else 'cpa-stack'
    if len(sys.argv) > 2 or not re.fullmatch(r'[a-z0-9][a-z0-9_-]{0,62}', project):
        emit('error', code='invalid_project_argument'); return 1
    ids = run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project='+project])
    if ids is None:
        emit('error', code='docker_unavailable'); return 1
    live = obj(run(['docker', 'inspect']+ids.split())) if ids.split() else []
    if not isinstance(live, list):
        emit('error', code='inspect_unavailable'); return 1
    required = ['cli-proxy-api', 'cpa-manager-plus', 'oauth-login', 'caddy']
    selected, chains = {}, set()
    emit('host', project=project, architecture=safe(os.uname().machine), compose_version=safe((run(['docker','compose','version','--short']) or '').strip(), r'[v0-9.]+'))
    for service in required:
        matches = [c for c in live if c.get('Config',{}).get('Labels',{}).get('com.docker.compose.service') == service and str(c.get('Config',{}).get('Labels',{}).get('com.docker.compose.oneoff','')).lower() != 'true']
        if len(matches) != 1:
            emit('service', service=service, selection='missing' if not matches else 'ambiguous', count=len(matches)); continue
        c = selected[service] = matches[0]
        labels = c.get('Config',{}).get('Labels',{}) or {}
        cwd, files = labels.get('com.docker.compose.project.working_dir',''), labels.get('com.docker.compose.project.config_files','')
        chain = files.split(',') if files else []
        if service != 'caddy' and cwd and chain and all(os.path.isabs(p) for p in [cwd]+chain): chains.add((cwd, tuple(chain)))
        image = obj(run(['docker','image','inspect',c['Image']]))
        revision = (image[0].get('Config',{}).get('Labels',{}) or {}).get('org.opencontainers.image.revision','') if image else ''
        mounts = [dict(type=safe(m.get('Type')), source=safe(m.get('Source')), target=safe(m.get('Destination')), name=safe(m.get('Name',''),r'[A-Za-z0-9_.-]*'), rw=m.get('RW') is True) for m in c.get('Mounts',[])]
        networks = [safe(n) for n in c.get('NetworkSettings',{}).get('Networks',{})]
        ports = [{'target':safe(p,r'[0-9]+/(tcp|udp)'), 'published':[{'ip':safe(x.get('HostIp'),r'[0-9a-fA-F:.]+'),'port':safe(x.get('HostPort'),r'[0-9]{1,5}')} for x in (bindings or [])]} for p,bindings in c.get('NetworkSettings',{}).get('Ports',{}).items()]
        emit('service', service=service, selection='unique', container_id=safe(c.get('Id'),r'[a-f0-9]{64}'), image_id=safe(c.get('Image'),r'sha256:[a-f0-9]{64}'), image_ref=safe(c.get('Config',{}).get('Image')), revision=safe(revision,r'[a-f0-9]{40}'), status=safe(c.get('State',{}).get('Status'),r'(running|exited|created|paused|restarting|dead|removing)'), health=safe(c.get('State',{}).get('Health',{}).get('Status','not_configured'),r'(healthy|unhealthy|starting|not_configured)'), working_dir=safe(cwd), compose_files=[safe(p) for p in chain], networks=networks, ports=ports, mounts=mounts)
    complete_chains=all(c.get('Config',{}).get('Labels',{}).get('com.docker.compose.project.working_dir') and c.get('Config',{}).get('Labels',{}).get('com.docker.compose.project.config_files') for c in selected.values())
    if len(selected) != 4 or len(chains) != 1 or not complete_chains:
        emit('complete', result='facts_incomplete', reason='service_or_compose_chain_ambiguous', write_release_ready=False); return 0
    root, files = next(iter(chains))
    command = ['docker','compose','--project-directory',root,'-p',project]
    for file in files: command += ['-f',file]
    resolved = obj(run(command+['config','--format','json']))
    emit('compose', parse='ok' if isinstance(resolved,dict) else 'unknown', services_match=set((resolved or {}).get('services',{}))==set(required), file_count=len(files))
    for path in files: emit('compose_file', path=safe(path), **path_info(path))
    for suffix in ['.mx-upgrade-20261110-rc1','.mx-upgrade-20261110-rc1/complete','.mx-upgrade-20261110-rc1/ready','.mx-upgrade-20261110-rc1/backup','.mx-upgrade-20261110-rc1/backup/data.tar.gz','.mx-upgrade-20261110-rc1/backup/environment-sources.tar.gz','.mx-upgrade-20261110-rc1/backup/contents.txt','cpa/bps-state','cpa-compose']:
        emit('path', role=suffix, **path_info(os.path.join(root,suffix)))
    try:
        disk=shutil.disk_usage(root); emit('disk', total_bytes=disk.total, used_bytes=disk.used, free_bytes=disk.free)
    except Exception: emit('disk', status='unknown')
    cpa, manager, worker, caddy = [selected[s] for s in required]
    config_path = host_path(cpa,'/CLIProxyAPI/config.yaml')
    config = yaml_doc(read(config_path),root) if config_path else None
    cfg = config if isinstance(config,dict) else {}
    auth_path = cfg.get('oauth',{}).get('auth-dir',cfg.get('auth-dir','/root/.cli-proxy-api'))
    protocol = cfg.get('bps-protocol',{}) or {}
    state_path = protocol.get('state-dir') or os.path.join(os.path.dirname(str(auth_path).replace('~/','/root/')),'bps-state')
    state_host = host_path(cpa,state_path)
    emit('bps_state', config_read='ok' if config else 'unknown', auth_dir=safe(auth_path), container_path=safe(state_path), mapped_host=safe(state_host), transport=protocol.get('image-transport') if protocol.get('image-transport') in ['native','relay'] else 'not_configured', relay_origin_configured=bool(protocol.get('image-relay-base-url')), existing_host=path_info(state_host) if state_host else {'exists':'unknown'})
    adapted = obj(run(['docker','exec',caddy['Id'],'caddy','adapt','--config','/etc/caddy/Caddyfile','--adapter','caddyfile']))
    relay=[]; generic_api=0
    def routes(value):
        nonlocal generic_api
        if isinstance(value,dict):
            for route in value.get('routes',[]):
                paths=[p for m in route.get('match',[]) for p in m.get('path',[])]
                if '/api/*' in paths: generic_api+=1
                if any('bps-images' in p for p in paths):
                    handlers=route.get('handle',[])
                    dials=[]
                    def gather(node):
                        if isinstance(node,dict):
                            if node.get('handler')=='reverse_proxy': dials.extend(u.get('dial') for u in node.get('upstreams',[]))
                            for child in node.values(): gather(child)
                        elif isinstance(node,list):
                            for child in node:gather(child)
                    gather(handlers)
                    methods=[x for m in route.get('match',[]) for x in m.get('method',[])]
                    relay.append(dict(exact_path=paths==['/api/bps-images/*'], methods_get_head=sorted(methods)==['GET','HEAD'], target_cpa=dials==['cli-proxy-api:8317']))
            for child in value.values(): routes(child)
        elif isinstance(value,list):
            for child in value:routes(child)
    routes(adapted)
    emit('caddy_routes', parse='ok' if isinstance(adapted,dict) else 'unknown', relay_routes=relay, generic_api_routes=generic_api, effective_order='requires_review' if relay else 'not_applicable')
    shared=set(cpa.get('NetworkSettings',{}).get('Networks',{})) & set(manager.get('NetworkSettings',{}).get('Networks',{})) & set(worker.get('NetworkSettings',{}).get('Networks',{}))
    network=next(iter(shared)) if len(shared)==1 else None
    def address(c): return c.get('NetworkSettings',{}).get('Networks',{}).get(network,{}).get('IPAddress') if network else None
    def secret(c,target):
        path=host_path(c,target)
        value=read(path) if path else None
        return value.strip() if value else None
    admin=secret(manager,'/run/secrets/cpamp-admin-key')
    worker_key=secret(worker,'/run/secrets/oauth_login_secret')
    keys=cfg.get('access',{}).get('api-keys',cfg.get('api-keys',[]))
    api_key=keys[0] if isinstance(keys,list) and keys and isinstance(keys[0],str) else None
    for name,c,port,path,key,expected in [('cpa',cpa,8317,'/v1/models',api_key,200),('manager',manager,18317,'/api/oauth-login/tasks/00000000-0000-4000-8000-000000000001',admin,404),('worker',worker,18417,'/health',worker_key,200)]:
        status,_=get(address(c),port,path,key)
        negative,_=get(address(c),port,path,'invalid-preflight-key')
        emit('authenticated_health', service=name, http_status=status, invalid_key_http_status=negative, result='passed' if status==expected and negative==401 else 'unknown')
    status,batch=get(address(manager),18317,'/api/oauth-login/batches/active',admin)
    active_status=batch.get('status') if isinstance(batch,dict) else None
    batch_count=0 if status==200 and batch is None else 1 if status==200 and active_status in ['running','cancelling'] else None
    emit('login_activity', batch_http_status=status, active_batches=batch_count, active_single_tasks=None, worker_active_tasks=None, single_task_reason='no_readonly_listing_api')
    status,snapshot=get(address(manager),18317,'/api/credential-concurrency',admin)
    credentials=snapshot.get('credentials') if isinstance(snapshot,dict) else None
    valid=status==200 and isinstance(snapshot,dict) and snapshot.get('mode')=='local' and type(snapshot.get('pool_waiting')) is int and snapshot['pool_waiting']>=0 and isinstance(credentials,list) and all(isinstance(x,dict) and type(x.get('active')) is int and x['active']>=0 and type(x.get('pinned_waiting')) is int and x['pinned_waiting']>=0 for x in credentials)
    emit('model_activity', http_status=status, active=sum(x['active'] for x in credentials) if valid else None, pinned_waiting=sum(x['pinned_waiting'] for x in credentials) if valid else None, pool_waiting=snapshot.get('pool_waiting') if valid and type(snapshot.get('pool_waiting')) is int else None, result='observed' if valid else 'unknown')
    emit('complete', result='read_only_facts_collected', write_release_ready=False, active_single_tasks='unknown')
    return 0

try: sys.exit(main())
except Exception:
    emit('error', code='preflight_incomplete'); sys.exit(1)
PY
