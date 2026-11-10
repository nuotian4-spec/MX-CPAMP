import http from 'node:http';
import {timingSafeEqual} from 'node:crypto';
import fs from 'node:fs';
import {pathToFileURL} from 'node:url';
import {Tasks} from './tasks.mjs';
export function createServer({secret,tasks=new Tasks()}={}){
  if(typeof secret!=='string'||secret.length<32)throw new Error('secret_configuration_invalid');
  const expected=Buffer.from('Bearer '+secret);
  const send=(res,status,payload)=>{res.writeHead(status,{'content-type':'application/json','cache-control':'no-store'});res.end(JSON.stringify(payload));};
  const server=http.createServer(async(req,res)=>{
    const supplied=Buffer.from(req.headers.authorization||'');
    if(supplied.length!==expected.length||!timingSafeEqual(supplied,expected)){req.resume();send(res,401,{error_code:'unauthorized'});return;}
    try{
      if(req.method==='GET'&&req.url==='/health'){send(res,200,{status:'ok'});return;}
      if(req.method==='DELETE'&&req.url==='/tasks'){await tasks.close();send(res,200,{status:'ok'});return;}
      if(req.method==='POST'&&req.url==='/tasks'){
        let size=0;const chunks=[];for await(const chunk of req){size+=chunk.length;if(size>16384){send(res,413,{error_code:'input_too_large'});return;}chunks.push(chunk);}
        let input;try{input=JSON.parse(Buffer.concat(chunks).toString());}catch{send(res,400,{error_code:'invalid_input'});return;}
        try{send(res,202,tasks.create(input));}finally{if(input&&typeof input==='object'){input.password='';input.totp_secret='';}}return;
      }
      const match=/^\/tasks\/([a-f0-9-]{36})$/.exec(req.url||'');
      if(match&&req.method==='GET'){const result=tasks.get(match[1]);send(res,result?200:404,result||{error_code:'not_found'});return;}
      if(match&&req.method==='DELETE'){const result=await tasks.delete(match[1]);send(res,result?200:404,result||{error_code:'not_found'});return;}
      req.resume();send(res,404,{error_code:'not_found'});
    }catch(error){send(res,[400,409].includes(error.status)?error.status:500,{error_code:['busy','invalid_input'].includes(error.message)?error.message:'internal_error'});}
  });
  server.requestTimeout=30000;server.headersTimeout=10000;server.tasks=tasks;return server;
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href){
  try{
    const secret=fs.readFileSync(process.env.OAUTH_LOGIN_SECRET_FILE||'/run/secrets/oauth_login_secret','utf8').trim();
    const server=createServer({secret});server.listen(18417,'0.0.0.0');
    const stop=()=>{server.close();server.tasks.close().finally(()=>process.exit(0));};process.on('SIGTERM',stop);process.on('SIGINT',stop);
  }catch{process.stderr.write('oauth_login_start_failed\n');process.exitCode=1;}
}
