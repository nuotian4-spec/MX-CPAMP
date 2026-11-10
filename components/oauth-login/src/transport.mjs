import {spawn} from 'node:child_process';
import {fileURLToPath} from 'node:url';
export class Transport {
  constructor() {
    this.headers={'user-agent':'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36','accept-language':'zh-CN,zh;q=0.9,en;q=0.8'};
    this.pending=new Map();this.sequence=0;this.buffer='';
  }
  start() {
    if(this.child)return;
    this.child=spawn('python3',[fileURLToPath(new URL('./tls_bridge.py',import.meta.url))],{stdio:['pipe','pipe','ignore'],windowsHide:true});
    this.child.stdout.setEncoding('utf8');
    this.child.stdout.on('data',chunk=>{this.buffer+=chunk;if(this.buffer.length>4*1024*1024){this.close();return;}let i;while((i=this.buffer.indexOf('\n'))>=0){const line=this.buffer.slice(0,i);this.buffer=this.buffer.slice(i+1);try{const message=JSON.parse(line);const pending=this.pending.get(message.id);if(!pending)continue;this.pending.delete(message.id);clearTimeout(pending.timer);message.ok?pending.resolve(message.result):pending.reject(new Error(message.error_code||'transport_error'));}catch{this.close();}}});
    this.child.on('error',()=>this.fail());this.child.on('close',()=>this.fail());
  }
  fail(){for(const p of this.pending.values()){clearTimeout(p.timer);p.reject(new Error('transport_error'));}this.pending.clear();}
  send(message){this.start();const id=++this.sequence;return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{this.pending.delete(id);reject(new Error('transport_error'));},120000);this.pending.set(id,{resolve,reject,timer});this.child.stdin.write(JSON.stringify({id,...message})+'\n');});}
  async fetch(url,options={}){
    const headers=[...new Headers(options.headers||{}).entries()];
    const result=await this.send({operation:'request',url,method:options.method||'GET',headers,body:options.body===undefined?null:Buffer.from(String(options.body)).toString('base64'),timeout_ms:30000});
    const h=new Headers(result.headers);h.rawSetCookie=result.headers.filter(([n])=>n.toLowerCase()==='set-cookie').map(([,v])=>v);h.transportCookies=result.cookies;
    return {status:result.status,ok:result.status>=200&&result.status<300,headers:h,text:async()=>Buffer.from(result.body,'base64').toString('utf8')};
  }
  sentinel(input){return this.send({operation:'sentinel',...input,userAgent:this.headers['user-agent']});}
  close(){this.child?.kill();this.fail();this.child=null;}
}
