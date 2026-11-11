import {createServer} from '../src/server.mjs';
import {Tasks} from '../src/tasks.mjs';
import {fileURLToPath} from 'node:url';
const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);
const config=JSON.parse(Buffer.concat(chunks));
const server=createServer({secret:config.secret,tasks:new Tasks({runner:fileURLToPath(new URL('./fixture-runner.mjs',import.meta.url)),timeoutMs:config.timeout_ms||10000})});
server.listen(0,'127.0.0.1',()=>process.stdout.write('http://127.0.0.1:'+server.address().port+'\n'));
const stop=()=>{server.close();server.tasks.close().finally(()=>process.exit(0));};process.on('SIGTERM',stop);process.on('SIGINT',stop);
