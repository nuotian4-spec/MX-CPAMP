import fs from 'node:fs';
const chunks=[];for await(const part of process.stdin)chunks.push(part);const input=JSON.parse(Buffer.concat(chunks));
if(input.email.startsWith('hold-')){await new Promise(()=>setInterval(()=>{},1000));}
const bps=input.profile==='bps',client=bps?'app_fnr0pYvVwwFDocDumLG3H2Bp':'app_EMoamEEZ73f0CkXaXp7hrann';
const exp=Math.floor(Date.now()/1000)+86400,account='isolated-integration-account';
const jwt=claims=>'e30.'+Buffer.from(JSON.stringify(claims)).toString('base64url')+'.synthetic';
const claims={client_id:client,aud:client,exp,email:input.email,sub:'isolated-integration-sub','https://api.openai.com/auth':{chatgpt_account_id:account,chatgpt_plan_type:'plus'}};
fs.writeSync(3,JSON.stringify({status:'succeeded',result:{profile:bps?'excel':'codex',client_id:client,access_token:jwt(claims),refresh_token:'isolated-refresh-'+input.profile,id_token:jwt(claims),account_id:account,email:input.email,expired:new Date(exp*1000).toISOString()}}));
input.password='';input.totp_secret='';
