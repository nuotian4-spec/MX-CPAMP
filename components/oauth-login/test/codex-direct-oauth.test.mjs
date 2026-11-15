import test from 'node:test';
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import {runLogin, PROFILES, failureCode} from '../src/profiles.mjs';

const jwt = value => 'e30.' + Buffer.from(JSON.stringify(value)).toString('base64url') + '.synthetic';

// 所有响应都在内存提供；记录实际 runLogin 请求，不访问官方网络。
function directOAuthFixture({authorizeChallenge=false,entry='email',interactionAt='',interactionPath='',wrongState=false,wrongClient=false,wrongEmail=false,noMfa=false,rejectAt=''}={}) {
  const calls = [];
  let state, challenge;
  const profile = PROFILES.codex;
  const callback = () => `${profile.redirect}?code=synthetic-code&state=${wrongState?'wrong-state':state}`;
  const workspace = () => ({page:{type:'workspace'},'oai-client-auth-session':{workspaces:[{id:'synthetic-account',kind:'organization'}]},continue_url:'https://auth.openai.com/workspace'});
  const interactionPayload = () => ({continue_url:'https://auth.openai.com'+interactionPath});
  const json = value => new Response(JSON.stringify(value), {headers:{'content-type':'application/json'}});
  const transport = {
    headers:{'user-agent':'synthetic-client'},
    sentinel:async ({deviceId}) => {assert.equal(deviceId,'synthetic-device');return {token:'synthetic-sentinel'};},
    closed:false,
    close(){ this.closed=true; },
    async fetch(url, options={}) {
      const u = new URL(url);
      calls.push({host:u.hostname,path:u.pathname,method:options.method||'GET'});
      if (u.hostname === 'chatgpt.com') return new Response('<html>Blocked</html>', {status:403,headers:{'content-type':'text/html','cf-mitigated':'challenge'}});
      if (u.pathname === '/oauth/authorize') {
        if(authorizeChallenge) return new Response('<html>Blocked</html>', {status:403,headers:{'content-type':'text/html','cf-mitigated':'challenge'}});
        assert.equal(u.searchParams.get('client_id'),profile.client_id);
        assert.equal(u.searchParams.get('redirect_uri'),profile.redirect);
        assert.equal(u.searchParams.get('code_challenge_method'),'S256');
        state=u.searchParams.get('state');challenge=u.searchParams.get('code_challenge');
        const destination=entry==='callback'?callback():entry==='session'?'https://auth.openai.com/choose-an-account':entry==='password'?'https://auth.openai.com/log-in/password':entry.startsWith('/')?'https://auth.openai.com'+entry:'https://auth.openai.com/log-in';
        const response=new Response(null,{status:302,headers:{location:destination}});
        response.headers.rawSetCookie=['oai-did=synthetic-device; Path=/; Secure','auth-session=synthetic-session; Path=/; Secure'];
        return response;
      }
      if(u.pathname!=='/oauth/token')assert.match(new Headers(options.headers).get('cookie'),/auth-session=synthetic-session/);
      if (u.pathname === '/choose-an-account') return new Response('<input name="session_id" value="us_synthetic_session">');
      if (u.pathname === '/api/accounts/session/select') {
        assert.deepEqual(JSON.parse(options.body),{session_id:'us_synthetic_session'});
        return json(interactionAt==='session'?interactionPayload():workspace());
      }
      if(['/email-verification','/add-phone','/about-you'].includes(u.pathname))return new Response('Additional interaction');
      if (u.pathname === '/log-in') return new Response('Email');
      if (u.pathname === '/api/accounts/authorize/continue') {
        assert.deepEqual(JSON.parse(options.body),{username:{kind:'email',value:'synthetic@example.test'}});
        return json({continue_url:'https://auth.openai.com/log-in/password'});
      }
      if (u.pathname === '/log-in/password') return new Response('Password');
      if (u.pathname === '/api/accounts/password/verify') {
        assert.equal(JSON.parse(options.body).password,'synthetic-password');
        if(rejectAt==='password')return new Response(JSON.stringify({error:'incorrect_password'}),{status:400,headers:{'content-type':'application/json'}});
        if(interactionAt==='password')return json(interactionPayload());
        if(noMfa)return json(workspace());
        return json({page:{type:'mfa_challenge'},'oai-client-auth-session':{mfa_challenge_factors:[{id:'factor-synthetic',factor_type:'totp'}]},continue_url:'https://auth.openai.com/mfa-challenge/factor-synthetic'});
      }
      if (u.pathname === '/api/accounts/mfa/issue_challenge') return json({});
      if (u.pathname === '/api/accounts/mfa/verify') {
        assert.match(JSON.parse(options.body).code,/^\d{6}$/);
        if(rejectAt==='totp')return new Response(JSON.stringify({error:'incorrect_code'}),{status:400,headers:{'content-type':'application/json'}});
        return json(interactionAt==='totp'?interactionPayload():workspace());
      }
      if (u.pathname === '/api/accounts/workspace/select') {
        assert.deepEqual(JSON.parse(options.body),{workspace_id:'synthetic-account'});
        if(interactionAt==='session_redirect')return json({continue_url:'https://auth.openai.com/session-continue'});
        return json({continue_url:callback()});
      }
      if(u.pathname==='/session-continue')return new Response(null,{status:302,headers:{location:'https://auth.openai.com'+interactionPath}});
      if (u.pathname === '/oauth/token') {
        const fields=new URLSearchParams(options.body);
        assert.equal(fields.get('code'),'synthetic-code');
        assert.equal(fields.get('client_id'),profile.client_id);
        assert.equal(fields.get('redirect_uri'),profile.redirect);
        assert.equal(crypto.createHash('sha256').update(fields.get('code_verifier')).digest('base64url'),challenge);
        return json({access_token:jwt({exp:Math.floor(Date.now()/1000)+3600,'https://api.openai.com/auth':{chatgpt_account_id:'synthetic-account'}}),refresh_token:'synthetic-refresh',id_token:jwt({aud:wrongClient?'wrong-client':profile.client_id,email:wrongEmail?'wrong@example.test':'synthetic@example.test'})});
      }
      throw new Error(`意外合成请求：${u.hostname}${u.pathname}`);
    },
  };
  return {transport,calls};
}

test('Codex OAuth 不依赖返回挑战403的 ChatGPT 首页',async()=>{
  const {transport,calls}=directOAuthFixture();
  const input={profile:'codex',email:'synthetic@example.test',password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  const stages=[];
  try {
    const result=await runLogin(input,transport,stage=>stages.push(stage));
    assert.equal(result.account_id,'synthetic-account');
    assert.equal(calls[0].path,'/oauth/authorize');
    assert.equal(calls.filter(call=>call.host==='chatgpt.com').length,0);
    assert.equal(calls.filter(call=>call.path==='/oauth/token').length,1);
    assert.ok(stages.includes('email'));
    assert.ok(stages.includes('password'));
    assert.ok(stages.includes('totp'));
    assert.ok(stages.includes('workspace_session'));
  } finally {
    assert.equal(transport.closed,true);
    assert.equal(input.password,'');
    assert.equal(input.totp_secret,'');
  }
});

for(const entry of ['password','session','callback'])test('Codex直入OAuth支持 '+entry,async()=>{
  const {transport,calls}=directOAuthFixture({entry});
  const input={profile:'codex',email:'synthetic@example.test',password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  const result=await runLogin(input,transport);
  assert.equal(result.account_id,'synthetic-account');
  assert.equal(calls.some(call=>call.host==='chatgpt.com'),false);
  assert.equal(calls.filter(call=>call.path==='/api/accounts/password/verify').length,entry==='password'?1:0);
  assert.equal(calls.filter(call=>call.path==='/api/accounts/session/select').length,entry==='session'?1:0);
  assert.equal(calls.filter(call=>call.path==='/oauth/token').length,1);
  assert.equal(transport.closed,true);assert.equal(input.password,'');assert.equal(input.totp_secret,'');
});

for(const mismatch of ['wrongState','wrongClient','wrongEmail'])test('Codex拒绝 '+mismatch,async()=>{
  const {transport,calls}=directOAuthFixture({[mismatch]:true});
  const input={profile:'codex',email:'synthetic@example.test',password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  await assert.rejects(runLogin(input,transport),new RegExp(mismatch==='wrongState'?'callback_invalid':'identity_mismatch'));
  assert.equal(calls.filter(call=>call.path==='/oauth/token').length,mismatch==='wrongState'?0:1);
  assert.equal(transport.closed,true);assert.equal(input.password,'');assert.equal(input.totp_secret,'');
});

for(const [interactionPath,reason] of [['/email-verification','email_verification_required'],['/add-phone','phone_verification_required'],['/about-you','account_setup_required']])for(const interactionAt of ['entry','password','totp','session'])test(`Codex ${interactionAt} ${reason} 保留人工交互原因`,async()=>{
  const {transport,calls}=directOAuthFixture({entry:interactionAt==='entry'?interactionPath:interactionAt==='session'?'session':'email',interactionAt,interactionPath});
  const input={profile:'codex',email:'synthetic@example.test',password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  await assert.rejects(runLogin(input,transport),error=>{
    assert.equal(failureCode(error),'needs_user_action');assert.equal(error.login_reason,reason);
    if(reason==='email_verification_required')assert.equal(error.login_stage,'email_otp');
    return true;
  });
  assert.equal(calls.filter(call=>call.path==='/oauth/token').length,0);
  assert.equal(calls.filter(call=>call.path==='/api/accounts/password/verify').length,['password','totp'].includes(interactionAt)?1:0);
  assert.equal(transport.closed,true);assert.equal(input.password,'');assert.equal(input.totp_secret,'');
});

test('Codex无需MFA时不制造TOTP步骤',async()=>{
  const {transport,calls}=directOAuthFixture({noMfa:true});
  const input={profile:'codex',email:'synthetic@example.test',password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  const stages=[];await runLogin(input,transport,stage=>stages.push(stage));
  assert.equal(stages.includes('totp'),false);assert.equal(calls.some(call=>call.path.startsWith('/api/accounts/mfa/')),false);
  assert.equal(transport.closed,true);assert.equal(input.password,'');assert.equal(input.totp_secret,'');
});

for(const rejectAt of ['password','totp'])test('Codex '+rejectAt+' 拒绝后不重试',async()=>{
  const {transport,calls}=directOAuthFixture({rejectAt});
  const input={profile:'codex',email:'synthetic@example.test',password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  await assert.rejects(runLogin(input,transport),error=>{
    assert.equal(failureCode(error),rejectAt+'_rejected');assert.equal(error.login_stage,rejectAt);return true;
  });
  assert.equal(calls.filter(call=>call.path==='/api/accounts/password/verify').length,1);
  assert.equal(calls.filter(call=>call.path==='/api/accounts/mfa/verify').length,rejectAt==='totp'?1:0);
  assert.equal(calls.filter(call=>call.path==='/oauth/token').length,0);
  assert.equal(transport.closed,true);assert.equal(input.password,'');assert.equal(input.totp_secret,'');
});

for(const [interactionPath,reason] of [['/email-verification','email_verification_required'],['/add-phone','phone_verification_required'],['/about-you','account_setup_required']])test(`Codex session继续地址302后的 ${reason} 不误标回调错误`,async()=>{
  const {transport,calls}=directOAuthFixture({entry:'session',interactionAt:'session_redirect',interactionPath});
  const input={profile:'codex',email:'synthetic@example.test',password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  await assert.rejects(runLogin(input,transport),error=>{
    assert.equal(failureCode(error),'needs_user_action');assert.equal(error.login_reason,reason);
    assert.equal(error.login_stage,reason==='email_verification_required'?'email_otp':'callback');return true;
  });
  assert.equal(calls.filter(call=>call.path==='/api/accounts/session/select').length,1);
  assert.equal(calls.filter(call=>call.path==='/api/accounts/workspace/select').length,1);
  assert.equal(calls.filter(call=>call.path==='/session-continue').length,1);
  assert.equal(calls.filter(call=>call.path==='/oauth/token').length,0);
  assert.equal(transport.closed,true);assert.equal(input.password,'');assert.equal(input.totp_secret,'');
});

test('Codex OAuth 入口自身挑战仍停止且不提交密码或TOTP',async()=>{
  const {transport,calls}=directOAuthFixture({authorizeChallenge:true});
  const input={profile:'codex',email:'synthetic@example.test',password:'synthetic-password',totp_secret:'JBSWY3DPEHPK3PXP'};
  await assert.rejects(runLogin(input,transport),error=>{
    assert.equal(error.login_stage,'authorize');
    assert.equal(error.login_reason,'security_check');
    assert.equal(error.http_status,403);
    assert.equal(error.endpoint_category,'oauth_authorize');
    assert.equal(error.security_check_subtype,'challenge_header');
    assert.equal(error.challenge_header_present,true);
    return true;
  });
  assert.deepEqual(calls.map(call=>call.path),['/oauth/authorize']);
  assert.equal(transport.closed,true);
  assert.equal(input.password,'');
  assert.equal(input.totp_secret,'');
});
