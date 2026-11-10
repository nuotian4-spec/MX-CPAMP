import fs from 'node:fs';
import http from 'node:http';
import {createServer} from '/app/src/server.mjs';
import {Tasks} from '/app/src/tasks.mjs';

const events=[],activeTasks=new Set(),upstream=new Map(),closed=[];
let maxActive=0;
class TrackedTasks extends Tasks{
  create(input){const result=super.create(input);activeTasks.add(result.id);maxActive=Math.max(maxActive,activeTasks.size);events.push({event:'created',id:result.id});return result;}
  finish(task,...args){super.finish(task,...args);if(activeTasks.delete(task.id))events.push({event:'finished',id:task.id,status:task.status});}
}
const tasks=new TrackedTasks({runner:'/checks/fixture-runner.mjs'});
const secret=fs.readFileSync('/run/secrets/oauth_login_secret','utf8').trim();
const worker=createServer({secret,tasks});worker.listen(18417,'127.0.0.1');
const keys=new Map([['Bearer phase2-config-one','one'],['Bearer phase2-config-two','two']]);
function json(res,status,body){res.writeHead(status,{'content-type':'application/json'});res.end(JSON.stringify(body));}
const fixture=http.createServer(async(req,res)=>{
  if(req.method==='GET'&&req.url==='/control'){
    json(res,200,{tasks:{events,max_active:maxActive},pending:[...upstream.values()].map(item=>({marker:item.marker,key:item.key,model:item.model})),closed});return;
  }
  const chunks=[];for await(const chunk of req)chunks.push(chunk);
  let body={};try{body=JSON.parse(Buffer.concat(chunks));}catch{}
  if(req.method==='POST'&&req.url==='/control/release'){
    const item=upstream.get(body.marker);
    if(!item){json(res,404,{status:'missing'});return;}
    json(item.res,200,{id:'chatcmpl-phase2',object:'chat.completion',created:1,model:item.model,choices:[{index:0,message:{role:'assistant',content:'synthetic'},finish_reason:'stop'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}});
    json(res,200,{status:'released'});return;
  }
  if(req.method!=='POST'||req.url!=='/v1/chat/completions'){json(res,404,{status:'unknown'});return;}
  const key=keys.get(req.headers.authorization),marker=body.messages?.[0]?.content;
  if(!key||!/^hold-[1-4]$/.test(marker)||!['fixture-model-a','fixture-model-b'].includes(body.model)){json(res,400,{error:{message:'invalid_fixture_request'}});return;}
  const item={marker,key,model:body.model,res};upstream.set(marker,item);
  res.once('close',()=>{if(upstream.get(marker)===item)upstream.delete(marker);closed.push({marker,key});});
});
fixture.listen(18517,'127.0.0.1');
process.on('SIGTERM',()=>{worker.close();fixture.close();for(const item of upstream.values())item.res.destroy();tasks.close().finally(()=>process.exit(0));});
