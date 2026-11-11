"""Local Linux/Docker/Compose fixture. Never targets a production project."""
import hashlib, json, os, pathlib, subprocess, sys

root=pathlib.Path(os.environ['FIXTURE_ROOT'])
project=os.environ['FIXTURE_PROJECT']
delivery=pathlib.Path('/delivery/phase2-readonly-preflight.sh')
sentinel='SECRET_SENTINEL_PREFLIGHT_42'
def command(args):
    p=subprocess.run(args,text=True,capture_output=True)
    if p.returncode: raise RuntimeError('fixture_command_failed:'+p.stderr.replace(sentinel,'[redacted]')[-1000:])
    return p.stdout
for directory in ['cpa/auth','manager-data','secrets','.mx-upgrade-20261110-rc1/backup']:
    (root/directory).mkdir(parents=True,exist_ok=True)
for name in ['cpamp-admin-key','oauth_login_secret']:(root/'secrets'/name).write_text(sentinel)
(root/'mode.txt').write_text('local')
(root/'cpa-compose').write_text(sentinel)
(root/'cpa/config.yaml').write_text(json.dumps({'config-version':8,'oauth':{'auth-dir':'/root/.cli-proxy-api'},'access':{'api-keys':[sentinel]},'management':{'secret-key':sentinel},'bps-protocol':{'state-dir':'/var/lib/cpa/bps-state','image-transport':'native'}}))
(root/'Caddyfile').write_text(':8080 {\n handle /api/* {\n reverse_proxy cpa-manager-plus:18317\n }\n handle /v1/* {\n reverse_proxy cli-proxy-api:8317\n }\n}\n')
for name in ['complete','ready','backup/data.tar.gz','backup/environment-sources.tar.gz','backup/contents.txt']:
    (root/'.mx-upgrade-20261110-rc1'/name).write_text(sentinel)
(root/'probe.py').write_text('''import http.server,json,os
class H(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  key=self.headers.get('Authorization')=='Bearer '+os.environ['SENTINEL']
  status=200 if key else 401
  body={'secret':os.environ['SENTINEL']}
  if key and self.path.startswith('/api/oauth-login/tasks/'):status=404
  if key and self.path=='/api/oauth-login/batches/active':body=None
  mode=open('/mode.txt').read()
  if key and self.path=='/api/credential-concurrency':
   body={'mode':mode,'credentials':[{'active':2,'pinned_waiting':1,'token':os.environ['SENTINEL']}],'pool_waiting':3}
   if mode=='home':body={'mode':'home','credentials':[]}
   if mode=='invalid-pool':body['mode']='local';body['pool_waiting']=-1
  if key and mode=='legacy' and self.path in ['/api/credential-concurrency','/api/oauth-login/batches/active']:status=404
  self.send_response(status);self.end_headers();self.wfile.write(json.dumps(body).encode())
 def log_message(self,*args):pass
http.server.HTTPServer(('0.0.0.0',int(os.environ['PORT'])),H).serve_forever()
''')
services={}
for service,port in [('cli-proxy-api',8317),('cpa-manager-plus',18317),('oauth-login',18417)]:
    mounts=[f'{root}/probe.py:/probe.py:ro',f'{root}/mode.txt:/mode.txt:ro']
    if service=='cli-proxy-api':mounts += [f'{root}/cpa/config.yaml:/CLIProxyAPI/config.yaml:ro',f'{root}/cpa/auth:/root/.cli-proxy-api']
    if service=='cpa-manager-plus':mounts += [f'{root}/secrets/cpamp-admin-key:/run/secrets/cpamp-admin-key:ro']
    if service=='oauth-login':mounts += [f'{root}/secrets/oauth_login_secret:/run/secrets/oauth_login_secret:ro']
    services[service]={'image':'python:3.13-alpine','entrypoint':['python3','/probe.py'],'environment':{'PORT':str(port),'SENTINEL':sentinel},'volumes':mounts,'labels':{'sentinel':sentinel}}
services['caddy']={'image':'caddy:2.10.2-alpine','volumes':[f'{root}/Caddyfile:/etc/caddy/Caddyfile:ro'],'environment':{'SENTINEL':sentinel},'ports':['127.0.0.1::8080']}
(root/'compose.yaml').write_text(json.dumps({'services':services}))
(root/'override.yaml').write_text(json.dumps({'services':{'oauth-login':{'environment':{'OVERRIDE_SECRET':sentinel}}}}))
compose=['docker','compose','--project-directory',str(root),'-p',project,'-f',str(root/'compose.yaml'),'-f',str(root/'override.yaml')]
def snapshot():
    files={str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in root.rglob('*') if p.is_file()}
    ids=command(compose+['ps','-aq']).split()
    live=json.loads(command(['docker','inspect']+ids))
    return files,sorted((c['Id'],c['Image'],c['State']['StartedAt']) for c in live)
try:
    command(compose[:-2]+['up','-d','--pull','never','caddy'])
    command(compose+['up','-d','--no-deps','--pull','never','cli-proxy-api','cpa-manager-plus','oauth-login'])
    command(['docker','network','connect',project+'_default',os.environ['HOSTNAME']])
    before=snapshot()
    p=subprocess.run(['sh',str(delivery),project],text=True,capture_output=True)
    after=snapshot()
    assert p.returncode==0 and not p.stderr and sentinel not in p.stdout
    rows=[json.loads(line) for line in p.stdout.splitlines()]
    assert before==after,'read_only_state_changed'
    assert len([x for x in rows if x['kind']=='service' and x['selection']=='unique'])==4
    assert all(len(x['compose_files'])==(1 if x['service']=='caddy' else 2) for x in rows if x['kind']=='service')
    assert [x for x in rows if x['kind']=='compose'][0]['parse']=='ok'
    assert len([x for x in rows if x['kind']=='authenticated_health' and x['result']=='passed'])==3
    assert [x for x in rows if x['kind']=='model_activity'][0]['active']==2
    assert [x for x in rows if x['kind']=='login_activity'][0]['active_batches']==0
    assert [x for x in rows if x['kind']=='caddy_routes'][0]['generic_api_routes']==1
    assert [x for x in rows if x['kind']=='bps_state'][0]['container_path']=='/var/lib/cpa/bps-state'
    assert [x for x in rows if x['kind']=='complete'][0]['write_release_ready'] is False
    print('linux_docker_compose_four_services=passed')
    print('multi_compose_chain=passed')
    print('caddy_preserved_original_chain=passed')
    print('top_level_explicit_bps_state_path=passed')
    print('secret_sentinel_redaction=passed')
    print('file_bytes_and_container_identity_unchanged=passed')
    print('authenticated_statuses_and_activity_counts=passed')
    print('single_task_unknown=preserved')
    (root/'bin').mkdir(exist_ok=True)
    (root/'bin/sudo').write_text('#!/bin/sh\nexec "$@"\n')
    (root/'bin/sudo').chmod(0o700)
    # The fixture already runs as root; only sudo elevation is a no-op.
    env=dict(os.environ,CPA_PREFLIGHT_PROJECT=project,PATH=str(root/'bin')+':'+os.environ['PATH'])
    short_before=snapshot()
    p=subprocess.run(['sh','/checks/short-preflight.sh'],env=env,text=True,capture_output=True)
    assert p.returncode==0 and not p.stderr and sentinel not in p.stdout
    rows=[json.loads(line) for line in p.stdout.splitlines()]
    assert len([x for x in rows if x['kind']=='service'])==4
    assert all(len(x['compose_files'])==(1 if x['service']=='caddy' else 2) for x in rows if x['kind']=='service')
    assert snapshot()==short_before
    caddy_row=[x for x in rows if x['kind']=='service' and x['service']=='caddy'][0]
    assert caddy_row['network_mode']!='unknown'
    assert any(p['target']=='8080/tcp' and p['published'][0]['ip']=='127.0.0.1' and p['published'][0]['port'].isdigit() for p in caddy_row['ports'] if p['published'])
    assert [x for x in rows if x['kind']=='path' and x['role']=='cpa-compose'][0]['exists'] is True
    print('short_entry_linux_docker_sentinel_readonly=passed')
    print('short_actual_port_network_mode_wrapper_metadata=passed')
    for mode in ['home','invalid-pool','legacy']:
        (root/'mode.txt').write_text(mode)
        p=subprocess.run(['sh',str(delivery),project],text=True,capture_output=True)
        assert p.returncode==0 and not p.stderr and sentinel not in p.stdout
        rows=[json.loads(line) for line in p.stdout.splitlines()]
        assert [x for x in rows if x['kind']=='model_activity'][0]['active'] is None
        if mode=='legacy':assert [x for x in rows if x['kind']=='login_activity'][0]['active_batches'] is None
    print('home_invalid_pool_and_legacy_404_unknown=passed')
    # Remove credentials from the observed host mapping without changing services.
    (root/'secrets/cpamp-admin-key').write_text('wrong-key')
    p=subprocess.run(['sh',str(delivery),project],text=True,capture_output=True)
    assert p.returncode==0 and sentinel not in p.stdout+p.stderr
    rows=[json.loads(line) for line in p.stdout.splitlines()]
    assert [x for x in rows if x['kind']=='model_activity'][0]['active'] is None
    assert [x for x in rows if x['kind']=='login_activity'][0]['active_batches'] is None
    print('unreadable_activity_not_zero=passed')
finally:
    subprocess.run(['docker','network','disconnect',project+'_default',os.environ['HOSTNAME']],capture_output=True)
    command(compose+['down','--volumes'])
