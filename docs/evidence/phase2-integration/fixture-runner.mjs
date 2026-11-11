import fs from 'node:fs';
import crypto from 'node:crypto';
const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);
const input=JSON.parse(Buffer.concat(chunks));
await new Promise(resolve=>setTimeout(resolve,150));
if(input.email.startsWith('fail-')){
  fs.writeSync(3,JSON.stringify({status:'failed',error_code:'password_rejected',stage:'password',reason:'password_rejected'}));
}else{
  const bps=input.profile==='bps',client=bps?'app_fnr0pYvVwwFDocDumLG3H2Bp':'app_EMoamEEZ73f0CkXaXp7hrann';
  const identity=crypto.createHash('sha256').update(input.email).digest('hex').slice(0,20);
  const exp=Math.floor(Date.now()/1000)+86400,account='phase2-account-'+identity;
  const jwt=claims=>'e30.'+Buffer.from(JSON.stringify(claims)).toString('base64url')+'.synthetic';
  const claims={client_id:client,aud:client,exp,email:input.email,sub:'phase2-sub-'+identity,'https://api.openai.com/auth':{chatgpt_account_id:account,chatgpt_plan_type:'plus'}};
  fs.writeSync(3,JSON.stringify({status:'succeeded',result:{profile:bps?'excel':'codex',client_id:client,access_token:jwt(claims),refresh_token:'phase2-refresh-'+identity,id_token:jwt(claims),account_id:account,email:input.email,expired:new Date(exp*1000).toISOString()}}));
}
input.email=input.password=input.totp_secret='';
