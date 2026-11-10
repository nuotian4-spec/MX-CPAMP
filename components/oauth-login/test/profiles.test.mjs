import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import {runLogin,PROFILES,failureCode} from '../src/profiles.mjs';
const jwt=value=>'e30.'+Buffer.from(JSON.stringify(value)).toString('base64url')+'.fake';
// 假上游通过真实HTTP提供认证分步响应；不访问官方网络。
async function upstream(provider,{challenge=false,wrongClient=false}={}){
  let state;let exchanges=0;let passwords=0;let mfas=0;const p=PROFILES[provider];
  const callback=()=>p.redirect+'?code=fake-code&state='+state;
  const server=http.createServer(async(req,res)=>{
    let body='';for await(const c of req)body+=c;const u=new URL(req.url,'http://fake');
    const json=(data)=>{res.setHeader('content-type','application/json');res.end(JSON.stringify(data));};
    const redirect=to=>{res.writeHead(302,{location:to});res.end();};
    if(u.pathname==='/oauth/authorize'||u.pathname==='/api/accounts/authorize'){state=u.searchParams.get('state');assert.equal(u.searchParams.get('client_id'),p.client_id);assert.ok(u.searchParams.get('code_challenge'));return redirect(provider==='bps'?'https://auth.openai.com/log-in/password':callback());}
    if(u.pathname==='/api/auth/providers')return json({});
    if(u.pathname==='/api/auth/csrf')return json({csrfToken:'fake-csrf'});
    if(u.pathname==='/api/auth/signin/openai')return json({url:'https://auth.openai.com/log-in/password'});
    if(u.pathname==='/log-in/password'){res.end('password page');return;}
    if(u.pathname==='/api/accounts/password/verify'){passwords++;assert.equal(JSON.parse(body).password,'only-once-password');if(challenge)return json({continue_url:'https://auth.openai.com/email-verification'});return json({page:{type:'mfa_challenge'},'oai-client-auth-session':{mfa_challenge_factors:[{id:'factor-1',factor_type:'totp'}]},continue_url:'https://auth.openai.com/mfa-challenge/factor-1'});}
    if(u.pathname==='/api/accounts/mfa/issue_challenge')return json({});
    if(u.pathname==='/api/accounts/mfa/verify'){mfas++;assert.match(JSON.parse(body).code,/^\d{6}$/);return json({page:{type:'workspace'},'oai-client-auth-session':{workspaces:[{id:'account-1',kind:'organization'}]},continue_url:'https://auth.openai.com/workspace'});}
    if(u.pathname==='/api/accounts/workspace/select')return json({continue_url:provider==='bps'?callback():'https://chatgpt.com/'});
    if(u.pathname==='/oauth/token'){exchanges++;const fields=new URLSearchParams(body);assert.equal(fields.get('client_id'),p.client_id);assert.equal(fields.get('code'),'fake-code');assert.ok(fields.get('code_verifier'));assert.equal(u.searchParams.get('unified'),provider==='bps'?'true':null);return json({access_token:jwt({exp:Math.floor(Date.now()/1000)+3600,client_id:p.client_id,'https://api.openai.com/auth':{chatgpt_account_id:'account-1'}}),refresh_token:'fake-refresh-'+provider,id_token:jwt({aud:wrongClient?'wrong-client':p.client_id,email:'admin@example.test'})});}
    res.setHeader('set-cookie',['__Host-next-auth.csrf-token=fake-csrf; Path=/; Secure','__Secure-next-auth.session-token=fake-session; Path=/; Secure']);res.end('home');
  });
  await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
  const transport={headers:{'user-agent':'fake-client'},sentinel:async()=>({token:'fake-sentinel'}),close(){},async fetch(url,options={}){const u=new URL(url);const response=await fetch(base+u.pathname+u.search,{method:options.method||'GET',headers:options.headers,body:options.body,redirect:'manual'});response.headers.rawSetCookie=response.headers.getSetCookie();return response;}};
  return {transport,counts:()=>({exchanges,passwords,mfas}),close:()=>new Promise(r=>server.close(r))};
}
for(const provider of ['codex','bps'])test(provider+' 单profile、TOTP、PKCE与独立token端点',async()=>{
  const fake=await upstream(provider);const input={profile:provider,email:'admin@example.test',password:'only-once-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  const original={log:console.log,warn:console.warn};console.log=console.warn=()=>{};
  try{const result=await runLogin(input,fake.transport);assert.equal(result.profile,provider==='bps'?'excel':'codex');assert.equal(result.refresh_token,'fake-refresh-'+provider);assert.equal(result.account_id,'account-1');assert.equal(input.password,'');assert.equal(input.totp_secret,'');assert.deepEqual(fake.counts(),{exchanges:1,passwords:1,mfas:1});}finally{Object.assign(console,original);await fake.close();}
});
test('额外验证停止，不取token、不自动重试',async()=>{const fake=await upstream('bps',{challenge:true});const old=console.log;console.log=()=>{};try{await assert.rejects(runLogin({profile:'bps',email:'admin@example.test',password:'only-once-password',totp_secret:'JBSWY3DPEHPK3PXP'},fake.transport),e=>failureCode(e)==='needs_user_action');assert.deepEqual(fake.counts(),{exchanges:0,passwords:1,mfas:0});}finally{console.log=old;await fake.close();}});
test('错误client身份拒绝',async()=>{const fake=await upstream('bps',{wrongClient:true});const old=console.log;console.log=()=>{};try{await assert.rejects(runLogin({profile:'bps',email:'admin@example.test',password:'only-once-password',totp_secret:'JBSWY3DPEHPK3PXP'},fake.transport),/identity_mismatch/);}finally{console.log=old;await fake.close();}});
