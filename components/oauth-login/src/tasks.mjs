import {spawn,spawnSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {fileURLToPath} from 'node:url';
import {safeStage,safeReason} from './diagnostics.mjs';
const terminal=new Set(['succeeded','failed','needs_user_action','cancelled','expired']);
export class Tasks {
  constructor({runner=fileURLToPath(new URL('./login.mjs',import.meta.url)),timeoutMs=25*60*1000,retentionMs=5*60*1000}={}){this.runner=runner;this.timeoutMs=timeoutMs;this.retentionMs=retentionMs;this.tasks=new Map();}
  create(input){
    this.prune();
    if([...this.tasks.values()].some(t=>!terminal.has(t.status)))throw Object.assign(new Error('busy'),{status:409});
    if(!input||typeof input!=='object'||!['codex','bps'].includes(input.profile)||typeof input.email!=='string'||!input.email.includes('@')||typeof input.password!=='string'||!input.password||typeof input.totp_secret!=='string'||!/^[A-Z2-7\s=]{16,160}$/i.test(input.totp_secret))throw Object.assign(new Error('invalid_input'),{status:400});
    if(input.task_id!==undefined&&!/^[a-f0-9-]{36}$/.test(input.task_id))throw Object.assign(new Error('invalid_input'),{status:400});
    if(input.task_id&&this.tasks.has(input.task_id))throw Object.assign(new Error('busy'),{status:409});
    const task={id:input.task_id||randomUUID(),status:'running',created_at:new Date().toISOString(),profile:input.profile};
    this.tasks.set(task.id,task);
    const child=spawn(process.execPath,[this.runner],{detached:process.platform!=='win32',stdio:['pipe','ignore','ignore','pipe'],windowsHide:true,env:{PATH:process.env.PATH,HOME:process.env.HOME,TMPDIR:process.env.TMPDIR,LANG:'C.UTF-8'}});
    task.child=child;task.output='';
    task.done=new Promise(resolve=>task.resolve=resolve);
    task.timer=setTimeout(()=>this.stop(task,'expired'),this.timeoutMs);task.timer.unref?.();
    child.stdio[3].setEncoding('utf8');child.stdio[3].on('data',chunk=>{task.output+=chunk;if(task.output.length>1024*1024)this.stop(task,'failed','output_invalid');});
    child.stdin.on('error',()=>{});
    child.on('error',()=>this.finish(task,'failed','process_failed'));
    child.on('close',()=>{
      this.killGroup(task);
      if(terminal.has(task.status))return;
      if(task.stopStatus){this.finish(task,task.stopStatus,task.stopCode);return;}
      try{
        const response=JSON.parse(task.output);
        task.stage=safeStage(response.stage);task.reason=safeReason(response.reason);
        if(response.status==='succeeded'&&this.validResult(response.result,task.profile)){task.result=Object.fromEntries(['profile','client_id','access_token','refresh_token','id_token','account_id','email','expired'].map(k=>[k,response.result[k]]));this.finish(task,'succeeded');}
        else if(response.status==='needs_user_action')this.finish(task,'needs_user_action','needs_user_action');
        else this.finish(task,'failed',this.errorCode(response.error_code));
      }catch{this.finish(task,'failed','output_invalid');}
    });
    child.stdin.end(JSON.stringify({profile:input.profile,email:input.email,password:input.password,totp_secret:input.totp_secret}));
    input.password='';input.totp_secret='';
    return this.view(task);
  }
  validResult(r,profile){return r&&r.profile===(profile==='bps'?'excel':'codex')&&r.client_id===(profile==='bps'?'app_fnr0pYvVwwFDocDumLG3H2Bp':'app_EMoamEEZ73f0CkXaXp7hrann')&&['access_token','refresh_token','id_token','account_id','email','expired'].every(k=>typeof r[k]==='string'&&r[k])&&Number.isFinite(Date.parse(r.expired));}
  errorCode(code){return ['needs_user_action','password_rejected','totp_rejected','callback_invalid','token_exchange_failed','credentials_incomplete','identity_mismatch','invalid_profile','transport_error','protocol_error','process_failed','output_invalid'].includes(code)?code:'protocol_error';}
  killGroup(task){if(!task.child?.pid)return;try{if(process.platform==='win32')spawnSync('taskkill.exe',['/PID',String(task.child.pid),'/T','/F'],{stdio:'ignore',windowsHide:true});else process.kill(-task.child.pid,'SIGKILL');}catch(error){if(error.code!=='ESRCH')throw error;}}
  stop(task,status,code){if(terminal.has(task.status)||task.stopStatus)return;task.stopStatus=status;task.stopCode=code;this.killGroup(task);}
  finish(task,status,code){if(terminal.has(task.status))return;clearTimeout(task.timer);task.status=status;if(code)task.error_code=this.errorCode(code);task.finished_at=Date.now();task.output='';task.child=null;task.resolve?.();task.retention=setTimeout(()=>{task.result=null;this.tasks.delete(task.id);},this.retentionMs);task.retention.unref?.();}
  view(task){return {id:task.id,profile:task.profile,status:task.status,created_at:task.created_at,...(task.stage?{stage:task.stage}:{}),...(task.reason?{reason:task.reason}:{}),...(task.error_code?{error_code:task.error_code}:{}),...(task.result?{result:task.result}:{})};}
  get(id){this.prune();const task=this.tasks.get(id);return task?this.view(task):null;}
  async delete(id){const task=this.tasks.get(id);if(!task)return null;if(terminal.has(task.status)){const view=this.view(task);delete view.result;task.result=null;clearTimeout(task.retention);this.tasks.delete(id);return {...view,result_cleared:true};}this.stop(task,'cancelled');await task.done;return this.view(task);}
  prune(){for(const [id,task]of this.tasks)if(terminal.has(task.status)&&Date.now()-task.finished_at>this.retentionMs){task.result=null;this.tasks.delete(id);}}
  async close(){await Promise.all([...this.tasks.values()].map(async task=>{if(!terminal.has(task.status)){this.stop(task,'cancelled');await task.done;}task.result=null;clearTimeout(task.retention);}));this.tasks.clear();}
}
