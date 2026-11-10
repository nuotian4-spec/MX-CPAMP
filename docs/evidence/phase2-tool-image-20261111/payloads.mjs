// 本地合成的 32×32 纯红色 PNG，无外部资源或业务数据。
export const redPNG='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAAKElEQVR4nO3NsQ0AAAzCMP5/un0CNkuZ41wybXsHAAAAAAAAAAAAxR4yw/wuPL6QkAAAAABJRU5ErkJggg==';
export function featureRequests(){
  const common={model:'gpt-5.6-sol',reasoning:{effort:'low'},stream:false};
  return[
    {feature:'tools',body:{...common,input:'Call ping once.',tools:[{type:'function',name:'ping',description:'Return pong without external effects.',parameters:{type:'object',properties:{},required:[],additionalProperties:false},strict:true}],tool_choice:{type:'function',name:'ping'}}},
    {feature:'image',body:{...common,input:[{role:'user',content:[{type:'input_text',text:'Name the dominant color in this image.'},{type:'input_image',image_url:redPNG}]}]}}
  ];
}
