// Excel 的单 profile PKCE 流程源自 Sub2API 供体，适用 LGPL-3.0；其余协议核心见 licenses。
import crypto from 'node:crypto';
import * as core from './vendor/protocol-core.mjs';
import {Transport} from './transport.mjs';
import {safeStage, failureReason, safeDiagnostics, responseDiagnostics} from './diagnostics.mjs';
export const PROFILES=Object.freeze({
  codex:{profile:'codex',client_id:'app_EMoamEEZ73f0CkXaXp7hrann',redirect:'http://localhost:1455/auth/callback'},
  bps:{profile:'excel',client_id:'app_fnr0pYvVwwFDocDumLG3H2Bp',redirect:'https://bps.openai.com/basispoints/extension/360590d7-f8f9-4d88-bf75-0edfe0a4b9f3/auth/callback'},
});
const authBase='https://auth.openai.com';
function callbackResult(url,profile,state){const u=new URL(url);const expected=new URL(profile.redirect);if(u.origin!==expected.origin||u.pathname!==expected.pathname||u.username||u.password||u.searchParams.get('state')!==state||!u.searchParams.get('code'))throw new Error('callback_invalid');return u.searchParams.get('code');}
const interaction = reason => Object.assign(new Error('interaction_required'), {diagnostic_reason:reason});
function codexInteraction(payload, mark) {
  const url=typeof payload==='string'?payload:core.getContinueUrl(payload);
  let path;try{path=new URL(url).pathname;}catch{}
  if(path==='/email-verification'){mark('email_otp');throw interaction('email_verification_required');}
  if(path==='/add-phone'||payload?.page?.type==='add_phone')throw interaction('phone_verification_required');
  if(path==='/about-you'||payload?.page?.type==='about_you')throw interaction('account_setup_required');
}
async function loginCodexOAuth(client,page,input,mark) {
  codexInteraction(page.finalUrl,mark);
  if(core.isAuthLoginPage(page.finalUrl)){
    if(new URL(page.finalUrl).origin!==authBase)throw interaction('unexpected_login_page');
    const deviceId=client.jar.value('oai-did',authBase)||crypto.randomUUID();
    if(!core.isPasswordLoginPage(page.finalUrl)){
      mark('email');
      const headers=await core.createSentinelHeaders(client,{authBase,deviceId,flow:'authorize_continue'});
      const {data}=await core.authJsonStep(client,authBase,'POST','/api/accounts/authorize/continue',{username:{kind:'email',value:input.email}},{referer:page.finalUrl,headers});
      codexInteraction(data,mark);
      const next=core.getContinueUrl(data);
      if(!core.isPasswordLoginPage(next)||new URL(next).origin!==authBase)throw interaction('unexpected_login_page');
      page=await client.follow(next,{endpointCategory:'auth_redirect'});
      codexInteraction(page.finalUrl,mark);
      if(!core.isPasswordLoginPage(page.finalUrl)||new URL(page.finalUrl).origin!==authBase)throw interaction('unexpected_login_page');
    }
    mark('password');
    let payload=await core.verifyPassword(client,{authBase,deviceId,password:input.password,referer:page.finalUrl});
    mark('authorize');
    codexInteraction(payload,mark);
    payload=await core.completeTotpMfaIfNeeded(client,{authBase,deviceId,payload,totpSecret:input.totp_secret,referer:page.finalUrl,onStage:mark});
    mark('authorize');
    codexInteraction(payload,mark);
    payload=await core.selectChatgptLoginWorkspaceIfNeeded(client,{authBase,payload,onStage:mark});
    mark('callback');
    codexInteraction(payload,mark);
    page=await core.continueFlow(client,payload);
  }
  if(page?.finalUrl?.startsWith(PROFILES.codex.redirect))return page;
  codexInteraction(page?.finalUrl,mark);
  mark('workspace_session');
  const session=core.extractFirstSessionId(page?.last?.text||'');
  if(!session)throw interaction('session_selection_required');
  let {data}=await core.authJsonStep(client,authBase,'POST','/api/accounts/session/select',{session_id:session},{referer:page.finalUrl});
  codexInteraction(data,mark);
  const workspace=core.pickWorkspaceId(data);
  if(workspace)({data}=await core.authJsonStep(client,authBase,'POST','/api/accounts/workspace/select',{workspace_id:workspace},{referer:core.getContinueUrl(data)||authBase}));
  codexInteraction(data,mark);
  if(!core.getContinueUrl(data))throw interaction('session_selection_required');
  mark('callback');
  page=await core.continueFlow(client,data);
  codexInteraction(page?.finalUrl,mark);
  return page;
}
export async function runLogin(input, transport=new Transport(), onStage) {
  let stage='input';
  const mark = value => { stage=safeStage(value); try { onStage?.(stage); } catch {} };
  const p=PROFILES[input.profile];if(!p)throw new Error('invalid_profile');
  const client=new core.ProtocolClient({transport});
  const verifier=crypto.randomBytes(48).toString('base64url');
  const state=(input.profile==='bps'?'bps.':'')+crypto.randomBytes(24).toString('base64url')+(input.profile==='bps'?'.PC':'');
  const query=new URLSearchParams({client_id:p.client_id,redirect_uri:p.redirect,response_type:'code',scope:input.profile==='bps'?'openid offline_access email profile organization.read':'openid profile email offline_access',state,code_challenge_method:'S256',code_challenge:crypto.createHash('sha256').update(verifier).digest('base64url')});
  if(input.profile==='bps'){query.set('audience','https://api.openai.com/v1');query.set('platform','PC');}else{query.set('codex_cli_simplified_flow','true');query.set('id_token_add_organizations','true');}
  try {
    mark('authorize');
    let page=await client.follow(authBase+(input.profile==='bps'?'/api/accounts/authorize?':'/oauth/authorize?')+query,{endpointCategory:'oauth_authorize'});
    if(input.profile==='bps'){
      const deviceId=client.jar.value('oai-did',authBase)||crypto.randomUUID();
      if(!core.isPasswordLoginPage(page.finalUrl)){
        if(!core.isAuthLoginPage(page.finalUrl))throw new Error('interaction_required');
        mark('email');
        const headers=await core.createSentinelHeaders(client,{authBase,deviceId,flow:'authorize_continue'});
        const {data}=await core.authJsonStep(client,authBase,'POST','/api/accounts/authorize/continue',{username:{kind:'email',value:input.email}},{referer:page.finalUrl,headers});
        const next=core.getContinueUrl(data);if(!core.isPasswordLoginPage(next))throw new Error('interaction_required');page=await client.follow(next,{endpointCategory:'auth_redirect'});
      }
      mark('password');
      let payload=await core.verifyPassword(client,{authBase,deviceId,password:input.password,referer:page.finalUrl});
      mark('authorize');
      payload=await core.completeTotpMfaIfNeeded(client,{authBase,deviceId,payload,totpSecret:input.totp_secret,referer:page.finalUrl,onStage:mark});
      mark('authorize');
      payload=await core.selectChatgptLoginWorkspaceIfNeeded(client,{authBase,payload,onStage:mark});
      mark('callback');
      page=await core.continueFlow(client,payload);
    }else page=await loginCodexOAuth(client,page,input,mark);
    mark('callback');
    const code=callbackResult(page?.finalUrl,p,state);
    mark('token_exchange');
    let response, tokenText, token;
    try {
      response=await transport.fetch(authBase+'/oauth/token'+(input.profile==='bps'?'?unified=true':''),{method:'POST',headers:{'content-type':'application/x-www-form-urlencoded'},body:new URLSearchParams({grant_type:'authorization_code',client_id:p.client_id,redirect_uri:p.redirect,code,code_verifier:verifier})});
      try { tokenText=await response.text(); } catch(error) { if(response.ok)throw error; }
      if(!response.ok)throw new Error('token_exchange_failed');
      token=JSON.parse(tokenText);
    } catch(error) { throw Object.assign(error,responseDiagnostics(response,tokenText,'oauth_token')); }
    mark('credential_validation');
    for(const name of ['access_token','refresh_token','id_token'])if(typeof token[name]!=='string'||!token[name])throw new Error('credentials_incomplete');
    const access=core.decodeJwtPayload(token.access_token);const identity=core.decodeJwtPayload(token.id_token);const auth=access['https://api.openai.com/auth']||{};
    const audience=Array.isArray(identity.aud)?identity.aud:[identity.aud];
    if(!audience.includes(p.client_id)||String(identity.email||'').toLowerCase()!==input.email.toLowerCase())throw new Error('identity_mismatch');
    if(input.profile==='bps'&&(access.client_id||access.azp)!==p.client_id)throw new Error('identity_mismatch');
    const accountID=auth.chatgpt_account_id||(identity['https://api.openai.com/auth']||{}).chatgpt_account_id;
    if(!accountID||!Number.isFinite(access.exp)||access.exp*1000<=Date.now())throw new Error('identity_mismatch');
    return {profile:p.profile,client_id:p.client_id,access_token:token.access_token,refresh_token:token.refresh_token,id_token:token.id_token,account_id:accountID,email:identity.email,expired:new Date(access.exp*1000).toISOString()};
  }catch(error){
    error.login_stage=stage;error.login_reason=failureReason(error,stage);Object.assign(error,safeDiagnostics(error));throw error;
  }finally{transport.close();input.password='';input.totp_secret='';}
}
export function failureCode(error){
  const text=String(error?.message||'');
  if(/interaction_required|security.check|PROXY_RISK_CONTROL|additional.verification|mfa.*required|unexpected.login|session.selection/i.test(text))return 'needs_user_action';
  for(const code of ['password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error'])if(text===code)return code;
  return 'protocol_error';
}
