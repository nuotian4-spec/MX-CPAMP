import fs from 'node:fs';
import {runLogin,failureCode} from './profiles.mjs';
// 授权材料只经过 stdin 与 fd 3；此进程的日志输出由父进程丢弃。
console.log=console.warn=console.error=()=>{};
let input;
try{const chunks=[];let size=0;for await(const chunk of process.stdin){size+=chunk.length;if(size>16384)throw new Error('invalid_input');chunks.push(chunk);}input=JSON.parse(Buffer.concat(chunks).toString());const result=await runLogin(input);fs.writeSync(3,JSON.stringify({status:'succeeded',result}));}
catch(error){const code=failureCode(error);fs.writeSync(3,JSON.stringify({status:code==='needs_user_action'?'needs_user_action':'failed',error_code:code}));}
finally{if(input){input.password='';input.totp_secret='';}}
