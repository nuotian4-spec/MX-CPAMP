// 本地合成的 32×32 纯红色 PNG，无外部资源或业务数据。
export const redPNG='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAAKElEQVR4nO3NsQ0AAAzCMP5/un0CNkuZ41wybXsHAAAAAAAAAAAAxR4yw/wuPL6QkAAAAABJRU5ErkJggg==';
export const model='gpt-6-luna';
const common=()=>({model,reasoning:{effort:'low'},stream:false});
export const toolPrompt='Call ping exactly once with no arguments. After receiving its result, reply with exactly pong.';
export function textRequest(){return{...common(),input:'Reply with exactly OK.'};}
export function toolRequest(){
  return{...common(),input:[{role:'user',content:[{type:'input_text',text:toolPrompt}]}],tools:[{type:'function',name:'ping',description:'Return pong without external effects.',parameters:{type:'object',properties:{},required:[],additionalProperties:false},strict:true}],tool_choice:'auto'};
}
export function toolResultRequest(firstOutput,callID){
  const first=toolRequest();
  return{...first,tool_choice:'none',input:[...first.input,...structuredClone(firstOutput),{type:'function_call_output',call_id:callID,output:'pong'}]};
}
export function imageRequest(){
  return{...common(),input:[{role:'user',content:[{type:'input_text',text:'Name the dominant color in this image. Reply with exactly one color word.'},{type:'input_image',image_url:redPNG}]}]};
}
