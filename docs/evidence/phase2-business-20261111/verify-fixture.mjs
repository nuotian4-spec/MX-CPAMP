import assert from 'node:assert/strict';
import {runBusiness,models} from './check.mjs';
import {sanitizeReceipt} from './diagnostics.mjs';

const supportedRequest={model:'gpt-5.6-sol',input:'Reply with OK.',reasoning:{effort:'low'},stream:false};
function assertRequest(body){
  for(const field of ['max_output_tokens','max_tokens','max_completion_tokens'])assert.equal(Object.hasOwn(body,field),false);
  assert.deepEqual(body,supportedRequest);
}
assertRequest(supportedRequest);
for(const field of ['max_output_tokens','max_tokens','max_completion_tokens'])assert.throws(()=>assertRequest({...supportedRequest,[field]:128}),assert.AssertionError);

async function scenario(mode='complete'){
  const needsAction=mode==='needs_action',mappingFailed=mode==='mapping_failed';
  const input={cpa:'synthetic-cpa',admin:'synthetic-admin',api:'synthetic-api',credentials:{email:'fixture@example.invalid',password:'SECRET_PASSWORD_MARKER',totp_secret:'SECRET_TOTP_MARKER'}};
  const posts={batch:0,refresh:0,model:0},reports=[];
  let batchID='',limit=null,refreshDone=false,success=0;
  const authID='bps-fixture@example.invalid.json',authIndex='b'.repeat(16),connection_ref='a'.repeat(32);
  const meta=()=>({type:'bps',profile:'excel',client_id:'app_fnr0pYvVwwFDocDumLG3H2Bp',email:'fixture@example.invalid',account_id:'fixture-account',access_token:refreshDone?'SECRET_ACCESS_NEW_MARKER':'SECRET_ACCESS_MARKER',refresh_token:'SECRET_REFRESH_MARKER',concurrency_limit:limit});
  const entry=()=>({id:authID,name:authID,auth_index:mode==='index_mismatch'?'c'.repeat(16):authIndex,provider:'bps',login_method:'2fa',success});
  const request=async(service,path,key,method='GET',body)=>{
    if(service==='manager'&&path==='/api/oauth-login/batches'&&method==='POST'){posts.batch++;batchID=body.batch_id;assert.equal(body.profile,'bps');assert.equal(body.items.length,1);return{status:200,value:{id:batchID}};}
    if(service==='manager'&&path==='/api/oauth-login/batches/'+batchID)return{status:200,value:{id:batchID,profile:'bps',status:mappingFailed?'interrupted':'completed',error_code:mappingFailed?'credential_mapping_unconfirmed':undefined,items:[{id:'fixture-task',auth_index:needsAction||mappingFailed?undefined:authIndex,...(mode==='raw_id'?{auth_id:authID}:{}),status:needsAction?'needs_user_action':mappingFailed?'failed':'succeeded',error_code:mappingFailed?'credential_mapping_unconfirmed':undefined,stage:'authorize',reason:needsAction?'security_check':undefined}]}};
    if(path==='/v8/management/credentials')return{status:200,value:{files:mode==='duplicate_index'?[entry(),{...entry(),id:'another-private-id'}]:[entry()]}};
    if(path.startsWith('/v8/management/credentials/download')){assert.equal(new URL('http://fixture'+path).searchParams.get('name'),authID);return{status:200,value:meta()};}
    if(path==='/api/credential-concurrency'){
      if(method==='PATCH'){assert.equal(body.connection_ref,connection_ref);assert.equal(body.auth_id,authID);limit=body.concurrency_limit;return{status:200,value:{status:'saved'}};}
      return{status:200,value:{mode:'local',default_limit:20,pool_waiting:0,connection_ref,credentials:[{auth_id:authID,provider:'bps',concurrency_limit:limit,effective_limit:limit??20,active:0,pinned_waiting:0}]}};
    }
    if(path==='/v8/management/credentials/refresh'&&method==='POST'){posts.refresh++;assert.deepEqual(body,{name:authID});refreshDone=true;return{status:200,value:{ok:true,auth:entry()}};}
    if(path==='/v1/models')return{status:200,value:{data:models.map(id=>({id}))}};
    if(path==='/v1/responses'&&method==='POST'){posts.model++;assertRequest(body);if(mode==='http500')return{status:500,value:{error:{code:'internal_server_error',type:'server_error',message:'SECRET_UPSTREAM_MESSAGE_MARKER https://fixture.invalid/SECRET_URL_MARKER'},debug:'SECRET_BODY_MARKER',upstream_status:401}};success++;return{status:200,value:{id:'resp_bps_fixture',status:'completed',output:[{type:'message',content:[{type:'output_text',text:'SECRET_OUTPUT_MARKER'}]}]}};}
    throw new Error('unexpected_fixture_call');
  };
  let result,error;
  try{result=await runBusiness(input,{request,report:value=>reports.push(value),wait:async()=>{},readWorker:()=>({})});}catch(value){error=value;}
  if(mode==='http500')assert.equal(error?.message,'public_request_rejected');
  else if(['index_mismatch','duplicate_index','raw_id'].includes(mode))assert.equal(error?.message,mode==='raw_id'?'batch_result_invalid':'registration_unconfirmed');
  else{assert.equal(error,undefined);assert.equal(result,mode==='complete');}
  assert.deepEqual(posts,['complete','http500'].includes(mode)?{batch:1,refresh:1,model:1}:{batch:1,refresh:0,model:0});
  if(mode==='http500'){
    const failure=reports.find(item=>item.stage==='business');assert.equal(failure.public_http_status,500);assert.equal(failure.public_error_code,'internal_server_error');assert.equal(failure.public_error_type,'server_error');assert.equal(failure.public_error_category,'unknown');assert.equal(failure.public_upstream_http_status,null);
    assert.equal(reports.some(item=>item.stage==='complete'),false);
  }
  if(mappingFailed)assert.ok(reports.some(item=>item.stage==='batch'&&item.error_code==='credential_mapping_unconfirmed'&&item.status==='failed'));
  assert.equal(JSON.stringify(reports).includes('SECRET_'),false);assert.equal(JSON.stringify(reports).includes('fixture@example.invalid'),false);
  assert.equal(JSON.stringify(reports).includes(authID),false);
  assert.equal(input.credentials.password,'');
  return reports;
}
const complete=await scenario();assert.equal(complete.at(-1).result,'passed');
for(const mode of ['needs_action','mapping_failed','index_mismatch','duplicate_index','raw_id','http500'])await scenario(mode);
const redacted=sanitizeReceipt({stage:'business',status:'SECRET_MARKER',error_code:'SECRET_MARKER',text:'SECRET_MARKER',http_status:403,model:'SECRET_MARKER',public_http_status:200,upstream_attempts:'unknown'});
assert.equal(JSON.stringify(redacted).includes('SECRET_MARKER'),false);assert.equal(redacted.public_http_status,200);assert.equal('http_status'in redacted,false);
process.stdout.write('business_fixture_budget_redaction=passed\n');
