import {Transport} from '/app/src/transport.mjs';
import {ProtocolClient} from '/app/src/vendor/protocol-core.mjs';

// 与真实2FA测试使用相同的本地代理注入，不传入任何账号材料。
process.env.HTTP_PROXY=process.env.HTTPS_PROXY='http://host.docker.internal:7890';
process.env.http_proxy=process.env.https_proxy=process.env.HTTPS_PROXY;
process.env.NO_PROXY=process.env.no_proxy='localhost,127.0.0.1';

for(const [target,url]of [['chatgpt_entry','https://chatgpt.com/'],['auth_entry','https://auth.openai.com/']]){
  const transport=new Transport();
  const originalFetch=transport.fetch.bind(transport);
  let http_status=null, security_check_page=false, error_code=null;
  transport.fetch=async(...args)=>{const response=await originalFetch(...args);http_status=response.status;return response;};
  try{
    const client=new ProtocolClient({transport});
    // request仅发一次GET，不跟随重定向；页面判别直接复用core逻辑。
    await client.request('GET',url);
  }catch(error){
    if(String(error?.message||'').startsWith('PROXY_RISK_CONTROL:'))security_check_page=true;
    else error_code='transport_error';
  }finally{transport.close();}
  process.stdout.write(JSON.stringify({target,http_status,security_check_page,error_code})+'\n');
}
