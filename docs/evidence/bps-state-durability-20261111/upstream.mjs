import fs from 'node:fs';
import https from 'node:https';
import http from 'node:http';

let calls=0,nativeRestored=false,sameIdentity=true;
const account='synthetic-account',token='SYNTHETIC_TOKEN_MARKER';
const server=https.createServer({cert:fs.readFileSync('/fixture/server.cert'),key:fs.readFileSync('/fixture/server.key')},async(req,res)=>{
  if(req.method!=='POST'||req.url!=='/basispoints/api/responses'){res.writeHead(404);res.end();return;}
  let raw='';for await(const part of req)raw+=part;
  calls++;
  sameIdentity&&=req.headers.authorization==='Bearer '+token&&req.headers['x-openai-account-id']===account;
  const body=JSON.parse(raw);
  const terminal={type:'response.completed',response:{id:'resp_synthetic',object:'response',status:'completed',model:'gpt-6-luna',output:[]}};
  if(calls===1){
    terminal.response.output=[{id:'fc_native',type:'function_call',call_id:'call_native',name:'run_officejs',arguments:JSON.stringify({summary:'Call client tool',extended_summary:'',destructive:false,references:[],code:JSON.stringify({name:'ping',arguments:{}})}),status:'completed'}];
  }else{
    nativeRestored=raw.includes('call_native')&&!raw.includes('call_bps_')&&raw.includes('pong');
    terminal.response.output=[{id:'msg_result',type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text:nativeRestored&&sameIdentity?'pong':'invalid fixture continuation'}]}];
  }
  res.writeHead(200,{'content-type':'text/event-stream'});res.end('data: '+JSON.stringify(terminal)+'\n\n');
});
server.listen(443,'0.0.0.0');
http.createServer((req,res)=>{res.writeHead(200,{'content-type':'application/json'});res.end(JSON.stringify({calls,nativeRestored,sameIdentity}));}).listen(8080,'0.0.0.0');
