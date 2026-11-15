import test from 'node:test';
import assert from 'node:assert/strict';
import {safeDiagnostics,responseDiagnostics} from '../src/diagnostics.mjs';
import {ProtocolClient,authJsonStep,createSentinelHeaders,verifyPassword,completeTotpMfaIfNeeded} from '../src/vendor/protocol-core.mjs';

const empty={http_status:null,endpoint_category:'unknown',security_check_subtype:'unknown',challenge_header_present:null};
const response=(status,headers={})=>({status,headers:new Headers(headers)});
test('诊断缺失和非法类型只返回unknown/null，不复制秘密字段',()=>{
  for(const value of [undefined,null,{},'secret-canary',{http_status:'403',endpoint_category:'https://example.test/?token=secret-canary',security_check_subtype:'secret-canary',challenge_header_present:'true',body:'secret-canary'}]) assert.deepEqual(safeDiagnostics(value),empty);
  for(const status of [99,600,403.5,NaN,Infinity])assert.equal(safeDiagnostics({http_status:status}).http_status,null);
  for(const status of [100,403,599])assert.equal(safeDiagnostics({http_status:status}).http_status,status);
  assert.deepEqual(responseDiagnostics(null,null,'chatgpt_home'),{...empty,endpoint_category:'chatgpt_home'});
});
const cases=[
  ['普通HTML403',403,{'content-type':'text/html'},'<html>Forbidden secret-canary</html>','html_forbidden_unclassified',false],
  ['明确挑战头',200,{'cf-mitigated':'challenge'},'secret-canary','challenge_header',true],
  ['另一挑战头',403,{'x-cf-mitigated':'challenge'},'','challenge_header',true],
  ['挑战HTML403',403,{},'Just a moment','challenge_html',false],
  ['挑战HTML400',400,{'content-type':'text/html'},'cdn-cgi','challenge_html',false],
  ['挑战HTML409',409,{},'<html>challenge-platform</html>','challenge_html',false],
  ['普通JSON403',403,{'content-type':'application/json'},'{"error":"forbidden"}','unknown',false],
  ['普通HTML409',409,{'content-type':'text/html'},'<html>Conflict</html>','unknown',false],
  ['挑战头存在但值为空',403,{'cf-mitigated':''},'Forbidden','unknown',true],
];
for(const [name,status,headers,body,subtype,present] of cases)test(name+'分类与协议异常使用同一契约',async()=>{
  const res=response(status,headers);
  const want={http_status:status,endpoint_category:'chatgpt_csrf',security_check_subtype:subtype,challenge_header_present:present};
  assert.deepEqual(responseDiagnostics(res,body,'chatgpt_csrf'),want);
  const client=new ProtocolClient({transport:{headers:{},fetch:async()=>({...res,text:async()=>body})}});
  if(subtype==='unknown')assert.deepEqual((await client.request('GET','https://chatgpt.com/api/auth/csrf',{endpointCategory:'chatgpt_csrf'})).diagnostic,want);
  else await assert.rejects(client.request('GET','https://chatgpt.com/api/auth/csrf?secret=secret-canary',{endpointCategory:'chatgpt_csrf'}),error=>{assert.deepEqual(safeDiagnostics(error),want);assert.ok(!JSON.stringify(safeDiagnostics(error)).includes('secret-canary'));return true;});
});
test('传输未观察到HTTP时仍保留固定请求类别',async()=>{
  const client=new ProtocolClient({transport:{headers:{},fetch:async()=>{throw new Error('transport_error')}}});
  await assert.rejects(client.request('GET','https://chatgpt.com/',{endpointCategory:'chatgpt_home'}),error=>{assert.deepEqual(safeDiagnostics(error),{...empty,endpoint_category:'chatgpt_home'});return true;});
});
test('固定认证步骤映射不输出URL与请求材料',async()=>{
  const mapping={'/api/accounts/authorize/continue':'oauth_authorize','/api/accounts/password/verify':'password_verify','/api/accounts/mfa/issue_challenge':'totp_issue','/api/accounts/mfa/verify':'totp_verify','/api/accounts/workspace/select':'workspace_select','/api/accounts/session/select':'session_select'};
  for(const [path,category]of Object.entries(mapping)){
    const client=new ProtocolClient({transport:{headers:{},fetch:async()=>({...response(403,{'cf-mitigated':'challenge'}),text:async()=>'secret-canary'})}});
    await assert.rejects(authJsonStep(client,'https://auth.openai.com','POST',path,{password:'secret-canary'}),error=>{assert.equal(error.endpoint_category,category);assert.equal(error.security_check_subtype,'challenge_header');assert.ok(!JSON.stringify(safeDiagnostics(error)).includes('secret-canary'));return true;});
  }
});
test('Sentinel异常只标类别，不伪造HTTP和挑战观察',async()=>{
  await assert.rejects(createSentinelHeaders({transport:{sentinel:async()=>{throw new Error('transport_error')}}},{authBase:'https://auth.openai.com'}),error=>{assert.deepEqual(safeDiagnostics(error),{...empty,endpoint_category:'sentinel'});return true;});
});
test('密码与TOTP拒绝映射保留HTTP诊断',async()=>{
  const client=new ProtocolClient({transport:{headers:{},sentinel:async()=>({token:'fake'}),fetch:async(url)=>({...response(url.endsWith('issue_challenge')?200:400,{'content-type':'application/json'}),text:async()=>url.endsWith('issue_challenge')?'{}':JSON.stringify({error:url.endsWith('password/verify')?'invalid_password':'invalid_totp'})})}});
  await assert.rejects(verifyPassword(client,{authBase:'https://auth.openai.com',password:'secret-canary'}),error=>{assert.equal(error.message,'password_rejected');assert.equal(error.http_status,400);assert.equal(error.endpoint_category,'password_verify');return true;});
  const old=console.log;console.log=()=>{};
  try{await assert.rejects(completeTotpMfaIfNeeded(client,{authBase:'https://auth.openai.com',totpSecret:'JBSWY3DPEHPK3PXP',payload:{page:{type:'mfa_challenge'},'oai-client-auth-session':{mfa_challenge_factors:[{id:'fake-factor',factor_type:'totp'}]}}}),error=>{assert.equal(error.message,'totp_rejected');assert.equal(error.http_status,400);assert.equal(error.endpoint_category,'totp_verify');return true;});}finally{console.log=old;}
});
