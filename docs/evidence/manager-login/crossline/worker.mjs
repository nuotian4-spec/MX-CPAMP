import fs from 'node:fs';
import {createServer} from '/app/src/server.mjs';
import {Tasks} from '/app/src/tasks.mjs';
const secret=fs.readFileSync('/run/secrets/oauth_login_secret','utf8').trim();
const server=createServer({secret,tasks:new Tasks({runner:'/checks/credential-runner.mjs'})});
server.listen(18417,'0.0.0.0');
process.on('SIGTERM',()=>{server.close();server.tasks.close().finally(()=>process.exit(0));});
