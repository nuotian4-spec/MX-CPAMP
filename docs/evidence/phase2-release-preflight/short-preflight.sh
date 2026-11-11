sudo python3 - "${CPA_PREFLIGHT_PROJECT:-cpa-stack}" <<'PREFLIGHT'
import json,os,pathlib,re,shutil,subprocess,sys
def out(kind,**v): print(json.dumps(dict(kind=kind,**v),ensure_ascii=True))
def call(args):
 p=subprocess.run(args,capture_output=True,text=True,timeout=15)
 if p.returncode: raise RuntimeError()
 return p.stdout
def safe(v): return v if isinstance(v,str) and re.fullmatch(r'[A-Za-z0-9_./:@+ -]{1,512}',v) else 'unknown'
def meta(p):
 try:
  s=p.stat();return dict(exists=True,mode=oct(s.st_mode&511)[2:],uid=s.st_uid,gid=s.st_gid,bytes=s.st_size)
 except FileNotFoundError:return dict(exists=False)
 except Exception:return dict(exists='unknown')
try:
 ids=call(['docker','ps','-aq','--filter','label=com.docker.compose.project='+sys.argv[1]]).split()
 live=json.loads(call(['docker','inspect']+ids)) if ids else []
 roots=set();chains=set();found=0
 for service in ['cli-proxy-api','cpa-manager-plus','oauth-login','caddy']:
  matches=[c for c in live if c.get('Config',{}).get('Labels',{}).get('com.docker.compose.service')==service and str(c.get('Config',{}).get('Labels',{}).get('com.docker.compose.oneoff','')).lower()!='true']
  if len(matches)!=1:out('service',service=service,count=len(matches),status='unknown');continue
  found+=1;c=matches[0];cfg=c.get('Config',{});labels=cfg.get('Labels',{}) or {};state=c.get('State',{})
  root=labels.get('com.docker.compose.project.working_dir','');files=labels.get('com.docker.compose.project.config_files','').split(',')
  if root and os.path.isabs(root):roots.add(root)
  chains.add((root,tuple(files)))
  image=json.loads(call(['docker','image','inspect',c['Image']]))[0]
  revision=(image.get('Config',{}).get('Labels',{}) or {}).get('org.opencontainers.image.revision','')
  mounts=[dict(type=safe(m.get('Type')),source=safe(m.get('Source')),target=safe(m.get('Destination')),rw=m.get('RW') is True) for m in c.get('Mounts',[])]
  networks=[safe(n) for n in c.get('NetworkSettings',{}).get('Networks',{})]
  ports=[dict(target=p,published=[dict(ip=b.get('HostIp'),port=b.get('HostPort')) for b in (v or []) if re.fullmatch(r'[0-9a-fA-F:.]+',b.get('HostIp','')) and re.fullmatch(r'[0-9]{1,5}',b.get('HostPort',''))]) for p,v in c.get('NetworkSettings',{}).get('Ports',{}).items() if re.fullmatch(r'[0-9]+/(tcp|udp)',p)]
  out('service',service=service,id=safe(c.get('Id')),image_id=safe(c.get('Image')),image_ref=safe(cfg.get('Image')),revision=revision if re.fullmatch('[a-f0-9]{40}',revision) else 'unknown',status=state.get('Status') if state.get('Status') in ['running','exited','created','paused','restarting','dead'] else 'unknown',health=state.get('Health',{}).get('Status') if state.get('Health',{}).get('Status') in ['healthy','unhealthy','starting'] else 'unknown',network_mode=safe(c.get('HostConfig',{}).get('NetworkMode')),networks=networks,ports=ports,mounts=mounts,working_dir=safe(root),compose_files=[safe(f) for f in files])
 out('compose',four_unique_services=found==4,chain_count=len(chains),single_working_dir=len(roots)==1,caddy_chain_may_differ=True)
 for root in sorted(roots):
  root=pathlib.Path(root);d=shutil.disk_usage(root);out('disk',root=safe(str(root)),free_bytes=d.free,total_bytes=d.total)
  for name in ['.mx-upgrade-20261110-rc1','.mx-upgrade-20261110-rc1/complete','.mx-upgrade-20261110-rc1/backup','.mx-upgrade-20261110-rc1/backup/data.tar.gz','.mx-upgrade-20261110-rc1/backup/environment-sources.tar.gz','cpa/bps-state','cpa-compose']:
   out('path',role=name,**meta(root/name))
 for root,files in sorted(chains):
  for file in files:
   if file and os.path.isabs(file):out('compose_file',path=safe(file),**meta(pathlib.Path(file)))
 out('activity',login='unknown',model_inflight='unknown',reason='not_observed_by_baseline_inventory')
 out('complete',result='read_only_baseline',write_release_ready=False)
except Exception:
 out('error',code='preflight_incomplete');raise SystemExit(1)
PREFLIGHT
