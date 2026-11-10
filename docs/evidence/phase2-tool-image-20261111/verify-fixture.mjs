import assert from 'node:assert/strict';
import {runBusiness,models} from './check.mjs';
import {sanitizeReceipt} from './diagnostics.mjs';

import {featureRequests,redPNG} from './payloads.mjs';
import fs from 'node:fs';
import zlib from 'node:zlib';

const common={model:'gpt-5.6-sol',reasoning:{effort:'low'},stream:false};
const expected=[
  {feature:'tools',body:{...common,input:'Call ping once.',tools:[{type:'function',name:'ping',description:'Return pong without external effects.',parameters:{type:'object',properties:{},required:[],additionalProperties:false},strict:true}],tool_choice:{type:'function',name:'ping'}}},
  {feature:'image',body:{...common,input:[{role:'user',content:[{type:'input_text',text:'Name the dominant color in this image.'},{type:'input_image',image_url:redPNG}]}]}}
];
assert.deepEqual(featureRequests(),expected);
assert.deepEqual(expected.map(item=>item.feature),['tools','image']);
const png=Buffer.from(redPNG.split(',')[1],'base64');
assert.deepEqual(png,fs.readFileSync(new URL('./red-32.png',import.meta.url)));
assert.equal(png.readUInt32BE(16),32);assert.equal(png.readUInt32BE(20),32);
let offset=8,idat=[];
while(offset<png.length){const length=png.readUInt32BE(offset),kind=png.toString('ascii',offset+4,offset+8);if(kind==='IDAT')idat.push(png.subarray(offset+8,offset+8+length));offset+=12+length;}
assert.deepEqual(zlib.inflateSync(Buffer.concat(idat)),Buffer.concat(Array.from({length:32},()=>Buffer.concat([Buffer.from([0]),Buffer.from(Array(32).fill([255,0,0]).flat())]))));
function assertRequest(body,index){
  for(const field of ['max_output_tokens','max_tokens','max_completion_tokens'])assert.equal(Object.hasOwn(body,field),false);
  assert.deepEqual(body,expected[index].body);
}
for(const field of ['max_output_tokens','max_tokens','max_completion_tokens'])assert.throws(()=>assertRequest({...expected[0].body,[field]:128},0),assert.AssertionError);

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
    if(path==='/v8/management/credentials/refresh'&&method==='POST'){posts.refresh++;assert.deepEqual(body,{name:authID});if(mode==='refresh_transport')throw new Error('SECRET_REFRESH_ERROR_MARKER');if(mode==='refresh_rejected')return{status:403,value:{error:{message:'SECRET_REFRESH_ERROR_MARKER'}}};refreshDone=true;return{status:200,value:{ok:true,auth:entry()}};}
    if(path==='/v1/models')return{status:200,value:{data:models.map(id=>({id}))}};
    if(path==='/v1/responses'&&method==='POST'){
      const index=posts.model++;assert.ok(index<2);assertRequest(body,index);
      const feature=expected[index].feature;
      if(mode===feature+'_transport')throw new Error('SECRET_TRANSPORT_MARKER');
      if(mode===feature+'_http400'||mode==='both_http400')return{status:400,value:{error:{code:'request_scoped',type:'invalid_request_error',message:'BPS supports OpenAI text requests only'},debug:'SECRET_BODY_MARKER'}};
      if(mode===feature+'_http403')return{status:403,value:{error:{code:'insufficient_quota',type:'permission_error',message:'SECRET_QUOTA_MARKER'}}};
      if(mode===feature+'_http500')return{status:500,value:{error:{code:'internal_server_error',type:'server_error',message:'SECRET_UPSTREAM_MESSAGE_MARKER https://fixture.invalid/SECRET_URL_MARKER'},debug:'SECRET_BODY_MARKER',upstream_status:401}};
      success++;
      return{status:200,value:{id:'resp_bps_fixture',status:mode===feature+'_invalid'?'failed':'completed',output:feature==='tools'?[{type:'function_call',name:'ping',arguments:'{}',call_id:'SECRET_CALL_ID_MARKER'}]:[{type:'message',content:[{type:'output_text',text:'SECRET_OUTPUT_MARKER'}]}]}};
    }
    throw new Error('unexpected_fixture_call');
  };
  let result,error;
  try{result=await runBusiness(input,{request,report:value=>reports.push(value),wait:async()=>{},readWorker:()=>({})});}catch(value){error=value;}
  const blocked=['needs_action','mapping_failed','index_mismatch','duplicate_index','raw_id','refresh_rejected','refresh_transport'].includes(mode);
  if(['index_mismatch','duplicate_index','raw_id','refresh_rejected','refresh_transport'].includes(mode))assert.equal(error?.message,mode==='raw_id'?'batch_result_invalid':mode==='refresh_rejected'?'refresh_rejected':mode==='refresh_transport'?'refresh_result_unconfirmed':'registration_unconfirmed');
  else{assert.equal(error,undefined);assert.equal(result,mode==='complete');}
  assert.deepEqual(posts,{batch:1,refresh:['refresh_rejected','refresh_transport'].includes(mode)||!blocked?1:0,model:blocked?0:2});
  const probes=reports.filter(item=>item.stage==='business');
  assert.deepEqual(probes.map(item=>item.feature),blocked?[]:['tools','image']);
  if(!blocked){
    assert.equal(reports.at(-1).probes_complete,true);assert.equal(reports.at(-1).client_requests,2);
    assert.equal(reports.at(-1).all_requests_succeeded,mode==='complete');
    for(const probe of probes)assert.equal(probe.client_requests,1);
    if(mode!=='complete')assert.ok(probes.some(item=>['rejected','unconfirmed','invalid'].includes(item.probe_outcome)));
  }
  if(mappingFailed)assert.ok(reports.some(item=>item.stage==='batch'&&item.error_code==='credential_mapping_unconfirmed'&&item.status==='failed'));
  assert.equal(JSON.stringify(reports).includes('SECRET_'),false);assert.equal(JSON.stringify(reports).includes('fixture@example.invalid'),false);
  assert.equal(JSON.stringify(reports).includes(authID),false);
  assert.equal(input.credentials.password,'');
  return reports;
}
const complete=await scenario();assert.equal(complete.at(-1).result,'passed');
for(const mode of ['needs_action','mapping_failed','index_mismatch','duplicate_index','raw_id','refresh_rejected','refresh_transport','tools_http400','image_http400','both_http400','tools_http403','image_http500','tools_transport','image_transport','tools_invalid','image_invalid'])await scenario(mode);
const redacted=sanitizeReceipt({stage:'business',status:'SECRET_MARKER',error_code:'SECRET_MARKER',text:'SECRET_MARKER',http_status:403,model:'SECRET_MARKER',public_http_status:200,upstream_attempts:'unknown'});
assert.equal(JSON.stringify(redacted).includes('SECRET_MARKER'),false);assert.equal(redacted.public_http_status,200);assert.equal('http_status'in redacted,false);
process.stdout.write('tool_image_fixture_budget_shortcircuit_redaction=passed\n');
