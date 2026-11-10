// 只替换 fetch，不访问网络；无凭证输入必须只检查健康与管理鉴权。
let calls=0;
globalThis.fetch=async(url,options={})=>{
  if(options.method!=='GET'||options.body)throw new Error('unexpected_preflight_write');
  calls++;
  if(url==='http://127.0.0.1:18317/health')return new Response('{"ok":true}',{status:200});
  if(url==='http://127.0.0.1:8317/v1/models')return new Response('{"data":[]}',{status:200});
  if(url==='http://127.0.0.1:18417/health')return new Response('{"ok":true}',{status:200});
  if(url==='http://127.0.0.1:18317/api/oauth-login/batches/active')return new Response('{}',{status:401});
  throw new Error('unexpected_preflight_endpoint');
};
process.on('exit',()=>{if(calls!==4)process.exitCode=23;});
