import assert from 'node:assert/strict';
import {runBusiness,models} from './check.mjs';
import {sanitizeReceipt} from './diagnostics.mjs';
import {toolRequest,toolResultRequest} from './payloads.mjs';

const toolBody={model:'gpt-6-luna',reasoning:{effort:'low'},stream:false,input:[{role:'user',content:[{type:'input_text',text:'Call ping exactly once with no arguments. After receiving its result, reply with exactly pong.'}]}],tools:[{type:'function',name:'ping',description:'Return pong without external effects.',parameters:{type:'object',properties:{},required:[],additionalProperties:false},strict:true}],tool_choice:'auto'};
assert.deepEqual(toolRequest(),toolBody);assert.deepEqual(models,['gpt-6-luna']);
async function scenario(mode='complete') {
  const input={cpa:'synthetic-cpa',admin:'synthetic-admin',api:'synthetic-api',credentials:{email:'fixture@example.invalid',password:'SECRET_PASSWORD_MARKER',totp_secret:'SECRET_TOTP_MARKER'}};
  const reports=[],features=[],posts={batch:0,model:0};
  let batchID='',success=0,firstOutput;
  const authID='bps-fixture@example.invalid.json',authIndex='b'.repeat(16),callID='SECRET_CALL_ID_MARKER';
  const entry=()=>({id:authID,auth_index:mode==='index_mismatch'?'c'.repeat(16):authIndex,provider:'bps',login_method:'2fa',success});
  const message=text=>({type:'message',role:'assistant',content:[{type:'output_text',text}]});
  const request=async(service,path,key,method='GET',body)=>{
    if(service==='manager'&&path==='/api/oauth-login/batches'&&method==='POST'){
      posts.batch++;batchID=body.batch_id;assert.equal(posts.batch,1);assert.equal(body.profile,'bps');assert.equal(body.items.length,1);
      assert.equal(body.items[0].password,'SECRET_PASSWORD_MARKER');
      if(mode==='create_transport')throw new Error('SECRET_TRANSPORT_MARKER');
      return{status:200,value:{id:batchID}};
    }
    if(service==='manager'&&path==='/api/oauth-login/batches/'+batchID)return{status:200,value:{id:batchID,profile:'bps',status:'completed',items:[{id:'fixture-task',auth_index:authIndex,status:mode==='needs_action'?'needs_user_action':'succeeded',...(mode==='raw_id'?{auth_id:authID}:{}),stage:'authorize',reason:mode==='needs_action'?'security_check':undefined}]}};
    if(path==='/v8/management/credentials')return{status:200,value:{files:mode==='duplicate_credential'?[entry(),{...entry(),id:'another-private-id'}]:[entry()]}};
    if(path.startsWith('/v8/management/credentials/download')){
      assert.equal(new URL('http://fixture'+path).searchParams.get('name'),authID);
      return{status:200,value:{type:'bps',profile:'excel',client_id:mode==='wrong_client'?'wrong':'app_fnr0pYvVwwFDocDumLG3H2Bp',email:'fixture@example.invalid',account_id:'SECRET_ACCOUNT_MARKER',access_token:'SECRET_ACCESS_MARKER',refresh_token:'SECRET_REFRESH_MARKER'}};
    }
    if(path==='/api/credential-concurrency'&&method==='GET')return{status:200,value:{mode:'local',pool_waiting:0,connection_ref:'a'.repeat(32),credentials:[{auth_id:authID,provider:'bps',effective_limit:20,active:0,pinned_waiting:0}]}};
    if(path==='/v1/models'&&method==='GET')return{status:200,value:{data:[{id:'gpt-6-luna'}]}};
    if(path==='/v1/responses'&&method==='POST'){
      posts.model++;assert.ok(posts.model<=2);
      const feature=body.input.some(item=>item.type==='function_call_output')?'tool_result':'tools';features.push(feature);
      const expected=feature==='tools'?toolBody:{...toolBody,tool_choice:'none',input:[...toolBody.input,...structuredClone(firstOutput),{type:'function_call_output',call_id:callID,output:'pong'}]};
      assert.deepEqual(body,expected);
      if(feature==='tool_result')assert.deepEqual(toolResultRequest(firstOutput,callID),expected);
      for(const field of ['previous_response_id','max_output_tokens','max_tokens','max_completion_tokens'])assert.equal(Object.hasOwn(body,field),false);
      if(mode===feature+'_transport')throw new Error('SECRET_TRANSPORT_MARKER');
      if(mode===feature+'_http403')return{status:403,value:{error:{code:'insufficient_quota',type:'permission_error',message:'SECRET_QUOTA_MARKER'}}};
      if(mode===feature+'_http502')return{status:502,value:{error:{code:'internal_server_error',type:'server_error',message:'SECRET_ERROR_MARKER',diagnostics:{stage:'upstream_inband',code:'upstream_failed',upstream_http_status:null,inband_status:403}},body:'SECRET_BODY_MARKER'}};
      success++;
      let output;
      if(feature==='tools'){
        const call={type:'function_call',name:mode==='tools_wrong_name'?'run_command':'ping',arguments:mode==='tools_args'?'["SECRET_COMMAND_MARKER"]':mode==='tools_bad_json'?'bad':'{}',call_id:mode==='tools_no_id'?'':callID,status:'completed'};
        output=mode==='tools_no_call'?[message('SECRET_NOT_A_TOOL_MARKER')]:[message('SECRET_INTRO_MARKER'),call];
        if(mode==='tools_multiple')output.push({...call,call_id:'SECRET_SECOND_CALL_MARKER'});
        firstOutput=structuredClone(output);
      }else output=mode==='tool_result_new_call'?[{type:'function_call',name:'ping',arguments:'{}',call_id:callID}]:[message(mode==='tool_result_semantics'?'not pong':'pong')];
      if(mode===feature+'_custom_call')output.push({type:'custom_tool_call',name:'SECRET_CUSTOM_NAME_MARKER',call_id:'SECRET_CUSTOM_CALL_MARKER',input:'SECRET_CUSTOM_INPUT_MARKER'});
      if(mode===feature+'_malformed')output={content:'SECRET_MALFORMED_MARKER'};
      return{status:200,value:{id:'resp_bps_fixture',status:mode===feature+'_invalid'?'failed':'completed',output}};
    }
    // 刷新、并发写入、文本和图片请求均不属于这份工具。
    throw new Error('unexpected_fixture_endpoint');
  };
  let result,error;
  try{result=await runBusiness(input,{request,report:value=>reports.push(value),wait:async()=>{},readWorker:()=>({})});}catch(value){error=value;}
  const blocked=['needs_action','index_mismatch','duplicate_credential','raw_id','wrong_client'].includes(mode);
  const wanted=blocked?[]:mode.startsWith('tools_')?['tools']:['tools','tool_result'];
  if(['index_mismatch','duplicate_credential','raw_id','wrong_client'].includes(mode))assert.equal(error?.message,mode==='raw_id'?'batch_result_invalid':mode==='wrong_client'?'saved_file_unconfirmed':'registration_unconfirmed');
  else{assert.equal(error,undefined);assert.equal(result,['complete','create_transport'].includes(mode));}
  assert.deepEqual(features,wanted);assert.deepEqual(posts,{batch:1,model:wanted.length});
  const probes=reports.filter(item=>item.stage==='business');assert.deepEqual(probes.map(item=>item.feature),wanted);
  if(!blocked){
    const summary=reports.at(-1);assert.equal(summary.client_requests,wanted.length);assert.equal(summary.tool_client_requests,wanted.length);assert.equal(summary.login_batch_requests,1);
    assert.equal(summary.all_requests_succeeded,['complete','create_transport'].includes(mode));
    assert.equal(summary.tool_roundtrip_complete,['complete','create_transport'].includes(mode));
  }
  const serialized=JSON.stringify(reports);
  for(const marker of ['SECRET_','fixture@example.invalid',authID,'data:image','"refresh_endpoint_requests"'])assert.equal(serialized.includes(marker),false);
  assert.equal(input.credentials.password,'');
  return reports;
}
for(const mode of ['complete','create_transport','needs_action','index_mismatch','duplicate_credential','raw_id','wrong_client','tools_transport','tools_http403','tools_http502','tools_invalid','tools_wrong_name','tools_args','tools_bad_json','tools_no_id','tools_no_call','tools_multiple','tools_malformed','tools_custom_call','tool_result_transport','tool_result_http403','tool_result_http502','tool_result_invalid','tool_result_semantics','tool_result_new_call','tool_result_malformed','tool_result_custom_call'])await scenario(mode);
const forged=sanitizeReceipt({stage:'business',feature:'image',client_requests:3,tool_client_requests:3,login_batch_requests:2,refresh_endpoint_requests:1,image_client_requests:1,message:'SECRET_MARKER',bps_diagnostic_stage:'SECRET_MARKER',bps_diagnostic_code:'SECRET_MARKER',bps_upstream_http_status:700,bps_inband_status:700});
assert.equal(JSON.stringify(forged).includes('SECRET_'),false);
for(const field of ['feature','client_requests','tool_client_requests','login_batch_requests','refresh_endpoint_requests','image_client_requests'])assert.equal(field in forged,false);
process.stdout.write('tool_only_budget_history_redaction=passed\n');
