import assert from 'node:assert/strict';
import {runBusiness,models} from './check.mjs';
import {sanitizeReceipt} from './diagnostics.mjs';

async function scenario(needsAction=false){
  const input={cpa:'synthetic-cpa',admin:'synthetic-admin',api:'synthetic-api',credentials:{email:'fixture@example.invalid',password:'SECRET_PASSWORD_MARKER',totp_secret:'SECRET_TOTP_MARKER'}};
  const posts={batch:0,refresh:0,model:0},reports=[];
  let batchID='',limit=null,refreshDone=false,success=0;
  const authID='fixture-bps.json',connection_ref='a'.repeat(32);
  const meta=()=>({type:'bps',profile:'excel',client_id:'app_fnr0pYvVwwFDocDumLG3H2Bp',email:'fixture@example.invalid',account_id:'fixture-account',access_token:refreshDone?'SECRET_ACCESS_NEW_MARKER':'SECRET_ACCESS_MARKER',refresh_token:'SECRET_REFRESH_MARKER',concurrency_limit:limit});
  const entry=()=>({id:authID,name:authID,provider:'bps',login_method:'2fa',success});
  const request=async(service,path,key,method='GET',body)=>{
    if(service==='manager'&&path==='/api/oauth-login/batches'&&method==='POST'){posts.batch++;batchID=body.batch_id;assert.equal(body.profile,'bps');assert.equal(body.items.length,1);return{status:200,value:{id:batchID}};}
    if(service==='manager'&&path==='/api/oauth-login/batches/'+batchID)return{status:200,value:{id:batchID,profile:'bps',status:'completed',items:[{id:'fixture-task',auth_id:authID,status:needsAction?'needs_user_action':'succeeded',stage:'authorize',reason:needsAction?'security_check':undefined}]}};
    if(path==='/v8/management/credentials')return{status:200,value:{files:[entry()]}};
    if(path.startsWith('/v8/management/credentials/download'))return{status:200,value:meta()};
    if(path==='/api/credential-concurrency'){
      if(method==='PATCH'){assert.equal(body.connection_ref,connection_ref);assert.equal(body.auth_id,authID);limit=body.concurrency_limit;return{status:200,value:{status:'saved'}};}
      return{status:200,value:{mode:'local',default_limit:20,pool_waiting:0,connection_ref,credentials:[{auth_id:authID,provider:'bps',concurrency_limit:limit,effective_limit:limit??20,active:0,pinned_waiting:0}]}};
    }
    if(path==='/v8/management/credentials/refresh'&&method==='POST'){posts.refresh++;assert.deepEqual(body,{name:authID});refreshDone=true;return{status:200,value:{ok:true,auth:entry()}};}
    if(path==='/v1/models')return{status:200,value:{data:models.map(id=>({id}))}};
    if(path==='/v1/responses'&&method==='POST'){posts.model++;assert.equal(body.model,'gpt-5.6-sol');assert.equal(body.max_output_tokens,128);assert.deepEqual(body.reasoning,{effort:'low'});success++;return{status:200,value:{id:'resp_bps_fixture',status:'completed',output:[{type:'message',content:[{type:'output_text',text:'SECRET_OUTPUT_MARKER'}]}]}};}
    throw new Error('unexpected_fixture_call');
  };
  const result=await runBusiness(input,{request,report:value=>reports.push(value),wait:async()=>{},readWorker:()=>({})});
  assert.equal(result,!needsAction);
  assert.deepEqual(posts,needsAction?{batch:1,refresh:0,model:0}:{batch:1,refresh:1,model:1});
  assert.equal(JSON.stringify(reports).includes('SECRET_'),false);assert.equal(JSON.stringify(reports).includes('fixture@example.invalid'),false);
  assert.equal(input.credentials.password,'');
  return reports;
}
const complete=await scenario(false);assert.equal(complete.at(-1).result,'passed');
await scenario(true);
const redacted=sanitizeReceipt({stage:'business',status:'SECRET_MARKER',error_code:'SECRET_MARKER',text:'SECRET_MARKER',http_status:403,model:'SECRET_MARKER',public_http_status:200,upstream_attempts:'unknown'});
assert.equal(JSON.stringify(redacted).includes('SECRET_MARKER'),false);assert.equal(redacted.public_http_status,200);assert.equal('http_status'in redacted,false);
process.stdout.write('business_fixture_budget_redaction=passed\n');
