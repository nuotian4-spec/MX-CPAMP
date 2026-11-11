import fs from 'node:fs';
import crypto from 'node:crypto';
const cpa=crypto.randomBytes(32).toString('hex'),admin=crypto.randomBytes(32).toString('hex'),api=crypto.randomBytes(32).toString('hex');
fs.mkdirSync('/cpa-state/auths',{recursive:true});
fs.writeFileSync('/cpa-state/config.yaml',JSON.stringify({
  'config-version':8,server:{host:'0.0.0.0',port:8317,'kv-store':{disabled:true}},
  management:{'allow-remote':true,'secret-key':cpa,'disable-control-panel':true,'disable-auto-update-panel':true},
  access:{'api-keys':[api]},oauth:{'auth-dir':'/state/auths',providers:{bps:{models:[]}}},
}),{mode:0o600});
fs.writeFileSync('/manager-config/config.json',JSON.stringify({httpAddr:'0.0.0.0:18317',dataDir:'/data',cpaUpstreamUrl:'http://127.0.0.1:8317',managementKeyFile:'/run/config/cpa-key',adminKeyFile:'/run/config/admin-key'}));
for(const [name,value]of [['cpa-key',cpa],['admin-key',admin],['api-key',api]])fs.writeFileSync('/manager-config/'+name,value,{mode:0o600});
const secret='/worker-secret/oauth_login_secret';fs.writeFileSync(secret,crypto.randomBytes(32).toString('hex'),{mode:0o440});fs.chownSync(secret,0,1000);fs.chmodSync(secret,0o440);
console.log('isolated_fake_configuration=prepared');
