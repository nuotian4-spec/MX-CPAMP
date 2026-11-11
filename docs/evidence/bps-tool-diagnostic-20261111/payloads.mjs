export const model='gpt-6-luna';
const common=()=>({model,reasoning:{effort:'low'},stream:false});
export const toolPrompt='Call ping exactly once with no arguments. After receiving its result, reply with exactly pong.';
export function toolRequest(){
  return{...common(),input:[{role:'user',content:[{type:'input_text',text:toolPrompt}]}],tools:[{type:'function',name:'ping',description:'Return pong without external effects.',parameters:{type:'object',properties:{},required:[],additionalProperties:false},strict:true}],tool_choice:'auto'};
}
export function toolResultRequest(firstOutput,callID){
  const first=toolRequest();
  return{...first,tool_choice:'none',input:[...first.input,...structuredClone(firstOutput),{type:'function_call_output',call_id:callID,output:'pong'}]};
}
