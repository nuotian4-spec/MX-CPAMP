import assert from 'node:assert/strict';
import fs from 'node:fs';
import zlib from 'node:zlib';
import {runBusiness,models} from './check.mjs';
import {sanitizeReceipt} from './diagnostics.mjs';
import {textRequest,toolRequest,toolResultRequest,imageRequest,redPNG} from './payloads.mjs';

const common={model:'gpt-6-luna',reasoning:{effort:'low'},stream:false};
const toolBody={...common,input:[{role:'user',content:[{type:'input_text',text:'Call ping exactly once with no arguments. After receiving its result, reply with exactly pong.'}]}],tools:[{type:'function',name:'ping',description:'Return pong without external effects.',parameters:{type:'object',properties:{},required:[],additionalProperties:false},strict:true}],tool_choice:'auto'};
const imageBody={...common,input:[{role:'user',content:[{type:'input_text',text:'Name the dominant color in this image. Reply with exactly one color word.'},{type:'input_image',image_url:redPNG}]}]};
assert.deepEqual(textRequest(),{...common,input:'Reply with exactly OK.'});
assert.deepEqual(toolRequest(),toolBody);assert.deepEqual(imageRequest(),imageBody);
assert.deepEqual(models,['gpt-6-luna']);
const png=Buffer.from(redPNG.split(',')[1],'base64');
assert.deepEqual(png,fs.readFileSync(new URL('./red-32.png',import.meta.url)));
assert.equal(png.readUInt32BE(16),32);assert.equal(png.readUInt32BE(20),32);
let offset=8,idat=[];
while(offset<png.length){const length=png.readUInt32BE(offset),kind=png.toString('ascii',offset+4,offset+8);if(kind==='IDAT')idat.push(png.subarray(offset+8,offset+8+length));offset+=12+length;}
assert.deepEqual(zlib.inflateSync(Buffer.concat(idat)),Buffer.concat(Array.from({length:32},()=>Buffer.concat([Buffer.from([0]),Buffer.from(Array(32).fill([255,0,0]).flat())]))));
function assertRequest(body,expected){
  for(const field of ['max_output_tokens','max_tokens','max_completion_tokens','previous_response_id'])assert.equal(Object.hasOwn(body,field),false);
  assert.deepEqual(body,expected);
}
for(const field of ['max_output_tokens','max_tokens','max_completion_tokens','previous_response_id'])assert.throws(()=>assertRequest({...toolBody,[field]:128},toolBody),assert.AssertionError);

async function scenario(mode='complete'){
  const needsAction=mode==='needs_action',mappingFailed=mode==='mapping_failed';
  const input={cpa:'synthetic-cpa',admin:'synthetic-admin',api:'synthetic-api',credentials:{email:'fixture@example.invalid',password:'SECRET_PASSWORD_MARKER',totp_secret:'SECRET_TOTP_MARKER'}};
  const posts={batch:0,refresh:0,model:0},reports=[],features=[];
  let batchID='',limit=null,refreshDone=false,success=0,firstOutput;
  const authID='bps-fixture@example.invalid.json',authIndex='b'.repeat(16),connection_ref='a'.repeat(32),callID='SECRET_ORIGINAL_CALL_ID_MARKER';
  const meta=()=>({type:'bps',profile:'excel',client_id:'app_fnr0pYvVwwFDocDumLG3H2Bp',email:'fixture@example.invalid',account_id:'fixture-account',access_token:refreshDone?'SECRET_ACCESS_NEW_MARKER':'SECRET_ACCESS_MARKER',refresh_token:'SECRET_REFRESH_MARKER',concurrency_limit:limit});
  const entry=()=>({id:authID,name:authID,auth_index:mode==='index_mismatch'?'c'.repeat(16):authIndex,provider:'bps',login_method:'2fa',success});
  const message=text=>({type:'message',role:'assistant',content:[{type:'output_text',text}]});
  const request=async(service,path,key,method='GET',body)=>{
    if(service==='manager'&&path==='/api/oauth-login/batches'&&method==='POST'){posts.batch++;batchID=body.batch_id;assert.equal(body.profile,'bps');assert.equal(body.items.length,1);return{status:200,value:{id:batchID}};}
    if(service==='manager'&&path==='/api/oauth-login/batches/'+batchID)return{status:200,value:{id:batchID,profile:'bps',status:mappingFailed?'interrupted':'completed',items:[{id:'fixture-task',auth_index:needsAction||mappingFailed?undefined:authIndex,...(mode==='raw_id'?{auth_id:authID}:{}),status:needsAction?'needs_user_action':mappingFailed?'failed':'succeeded',error_code:mappingFailed?'credential_mapping_unconfirmed':undefined,stage:'authorize',reason:needsAction?'security_check':undefined}]}};
    if(path==='/v8/management/credentials')return{status:200,value:{files:mode==='duplicate_index'?[entry(),{...entry(),id:'another-private-id'}]:[entry()]}};
    if(path.startsWith('/v8/management/credentials/download')){assert.equal(new URL('http://fixture'+path).searchParams.get('name'),authID);return{status:200,value:meta()};}
    if(path==='/api/credential-concurrency'){
      if(method==='PATCH'){assert.equal(body.connection_ref,connection_ref);assert.equal(body.auth_id,authID);limit=body.concurrency_limit;return{status:200,value:{status:'saved'}};}
      return{status:200,value:{mode:'local',pool_waiting:0,connection_ref,credentials:[{auth_id:authID,provider:'bps',concurrency_limit:limit,effective_limit:limit??20,active:0,pinned_waiting:0}]}};
    }
    if(path==='/v8/management/credentials/refresh'&&method==='POST'){
      posts.refresh++;assert.deepEqual(body,{name:authID});
      if(mode==='refresh_transport')throw new Error('SECRET_REFRESH_ERROR_MARKER');
      if(mode==='refresh_rejected')return{status:403,value:{error:{message:'SECRET_REFRESH_ERROR_MARKER'}}};
      refreshDone=true;return{status:200,value:{ok:true,auth:entry()}};
    }
    if(path==='/v1/models')return{status:200,value:{data:models.map(id=>({id}))}};
    if(path==='/v1/responses'&&method==='POST'){
      posts.model++;assert.ok(posts.model<=4);
      const feature=typeof body.input==='string'?'text':body.input.some(item=>item.type==='function_call_output')?'tool_result':body.tools?'tools':'image';
      assert.equal(features.includes(feature),false);features.push(feature);
      if(feature==='text')assertRequest(body,{...common,input:'Reply with exactly OK.'});
      if(feature==='tools')assertRequest(body,toolBody);
      if(feature==='image')assertRequest(body,imageBody);
      if(feature==='tool_result'){
        const expected={...toolBody,tool_choice:'none',input:[...toolBody.input,...structuredClone(firstOutput),{type:'function_call_output',call_id:callID,output:'pong'}]};
        assertRequest(body,expected);assert.deepEqual(toolResultRequest(firstOutput,callID),expected);
      }
      if(mode===feature+'_transport')throw new Error('SECRET_TRANSPORT_MARKER');
      if(mode===feature+'_http400')return{status:400,value:{error:{code:'request_scoped',type:'invalid_request_error',message:'client tools require the original provider'},debug:'SECRET_BODY_MARKER'}};
      if(mode===feature+'_http403')return{status:403,value:{error:{code:'insufficient_quota',type:'permission_error',message:'SECRET_QUOTA_MARKER'}}};
      if(mode===feature+'_http500')return{status:500,value:{error:{code:'internal_server_error',type:'server_error',message:'SECRET_UPSTREAM_MESSAGE_MARKER https://fixture.invalid/SECRET_URL_MARKER'},debug:'SECRET_BODY_MARKER',upstream_status:401}};
      success++;
      let output;
      if(feature==='tools'){
        const call={type:'function_call',id:'SECRET_ITEM_ID_MARKER',name:mode==='tools_wrong_name'?'run_command':'ping',arguments:mode==='tools_nonempty_args'?'{"command":"SECRET_COMMAND_MARKER"}':mode==='tools_bad_args'?'invalid':'{}',call_id:mode==='tools_missing_call_id'?'':callID,status:'completed'};
        output=mode==='tools_no_call'?[message('SECRET_NON_TOOL_MARKER')]:[message('SECRET_TOOL_INTRO_MARKER'),call];
        if(mode==='tools_multiple_calls')output.push({...call,call_id:'SECRET_SECOND_CALL_MARKER'});
        firstOutput=structuredClone(output);
      }else output=[message(feature==='text'?(mode==='text_semantics'?'NOT OK':'OK'):feature==='tool_result'?(mode==='tool_result_semantics'?'NOT pong':'pong'):mode==='image_wrong_color'?'blue':mode==='image_negated_red'?'not red':mode==='image_multicolor'?'red blue':'red')];
      if(mode===feature+'_tool_only')output=[{type:'function_call',name:'ping',arguments:'{}',call_id:callID}];
      if(mode===feature+'_malformed')output={content:'SECRET_MALFORMED_MARKER'};
      return{status:200,value:{id:'resp_bps_fixture',status:mode===feature+'_invalid'?'failed':'completed',output}};
    }
    throw new Error('unexpected_fixture_call');
  };
  let result,error;
  try{result=await runBusiness(input,{request,report:value=>reports.push(value),wait:async()=>{},readWorker:()=>({})});}catch(value){error=value;}
  const blocked=['needs_action','mapping_failed','index_mismatch','duplicate_index','raw_id','refresh_rejected','refresh_transport'].includes(mode);
  const textFailed=mode.startsWith('text_'),toolFailed=mode.startsWith('tools_');
  const wanted=blocked?[]:textFailed?['text']:toolFailed?['text','tools','image']:['text','tools','tool_result','image'];
  if(['index_mismatch','duplicate_index','raw_id','refresh_rejected','refresh_transport'].includes(mode))assert.equal(error?.message,mode==='raw_id'?'batch_result_invalid':mode==='refresh_rejected'?'refresh_rejected':mode==='refresh_transport'?'refresh_result_unconfirmed':'registration_unconfirmed');
  else{assert.equal(error,undefined);assert.equal(result,mode==='complete');}
  assert.deepEqual(posts,{batch:1,refresh:['refresh_rejected','refresh_transport'].includes(mode)||!blocked?1:0,model:wanted.length});
  assert.deepEqual(features,wanted);
  const probes=reports.filter(item=>item.stage==='business');assert.deepEqual(probes.map(item=>item.feature),wanted);
  if(!blocked){
    const summary=reports.at(-1);assert.equal(summary.client_requests,wanted.length);assert.equal(summary.text_gate_passed,!textFailed);
    assert.equal(summary.tool_client_requests,textFailed?0:toolFailed?1:2);assert.equal(summary.image_client_requests,textFailed?0:1);
    assert.equal(summary.all_requests_succeeded,mode==='complete');
    assert.equal(summary.result,mode==='complete'?'passed':undefined);
    for(const probe of probes)assert.equal(probe.client_requests,1);
    if(mode!=='complete')assert.ok(probes.some(item=>['rejected','unconfirmed','invalid'].includes(item.probe_outcome)));
    if(mode.startsWith('image_'))assert.equal(summary.image_red_confirmed,false);
  }
  if(mappingFailed)assert.ok(reports.some(item=>item.stage==='batch'&&item.error_code==='credential_mapping_unconfirmed'&&item.status==='failed'));
  const serialized=JSON.stringify(reports);
  for(const marker of ['SECRET_','fixture@example.invalid',authID,'data:image'])assert.equal(serialized.includes(marker),false);
  for(const report of reports)for(const field of ['text','output','input','arguments','call_id','url','message','access_token','refresh_token','password','totp_secret'])assert.equal(field in report,false);
  assert.equal(input.credentials.password,'');
  return reports;
}
const complete=await scenario();assert.equal(complete.at(-1).result,'passed');
for(const mode of ['needs_action','mapping_failed','index_mismatch','duplicate_index','raw_id','refresh_rejected','refresh_transport','text_http403','text_http400','text_http500','text_transport','text_invalid','text_semantics','text_tool_only','text_malformed','tools_http400','tools_http403','tools_http500','tools_transport','tools_invalid','tools_no_call','tools_wrong_name','tools_nonempty_args','tools_bad_args','tools_missing_call_id','tools_multiple_calls','tools_malformed','tool_result_http403','tool_result_http500','tool_result_transport','tool_result_invalid','tool_result_semantics','tool_result_tool_only','tool_result_malformed','image_http400','image_http403','image_http500','image_transport','image_invalid','image_wrong_color','image_negated_red','image_multicolor','image_tool_only','image_malformed'])await scenario(mode);
const redacted=sanitizeReceipt({stage:'business',feature:'SECRET_MARKER',status:'SECRET_MARKER',error_code:'SECRET_MARKER',text:'SECRET_MARKER',http_status:403,model:'SECRET_MARKER',public_http_status:200,upstream_attempts:'unknown',client_requests:5,tool_client_requests:3,image_client_requests:2});
assert.equal(JSON.stringify(redacted).includes('SECRET_MARKER'),false);assert.equal(redacted.public_http_status,200);
for(const field of ['http_status','client_requests','tool_client_requests','image_client_requests'])assert.equal(field in redacted,false);
process.stdout.write('text_tool_roundtrip_image_budget_semantics_redaction=passed\n');
