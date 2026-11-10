import fs from 'node:fs';
import {spawn} from 'node:child_process';
const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);const input=JSON.parse(Buffer.concat(chunks));
if(input.email.startsWith('hold')){const grandchild=spawn(process.execPath,['-e','setInterval(()=>{},1000)'],{stdio:'ignore'});fs.writeSync(3,JSON.stringify({pid:process.pid,grandchild:grandchild.pid}));setInterval(()=>{},1000);}
else if(input.email.startsWith('challenge')){fs.writeSync(3,JSON.stringify({status:'needs_user_action',error_code:'needs_user_action',stage:'totp',reason:'security_check'}));}
else if(input.email.startsWith('bad')){process.stdout.write(input.password);process.stderr.write(input.totp_secret);fs.writeSync(3,JSON.stringify({status:'failed',error_code:input.password,stage:input.password,reason:input.totp_secret}));}
else{fs.writeSync(3,JSON.stringify({status:'succeeded',result:{profile:input.profile==='bps'?'excel':'codex',client_id:input.profile==='bps'?'app_fnr0pYvVwwFDocDumLG3H2Bp':'app_EMoamEEZ73f0CkXaXp7hrann',access_token:'fake-access',refresh_token:'fake-refresh',id_token:'fake-id',account_id:'fake-account',email:input.email,expired:'2030-01-01T00:00:00Z'}}));}
