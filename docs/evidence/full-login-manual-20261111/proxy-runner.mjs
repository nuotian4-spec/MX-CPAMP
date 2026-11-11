import fs from 'node:fs';

// Tasks过滤父环境后，由执行进程自身设置本机代理。
process.env.HTTP_PROXY = process.env.HTTPS_PROXY = 'http://host.docker.internal:7890';
process.env.http_proxy = process.env.https_proxy = process.env.HTTPS_PROXY;
process.env.NO_PROXY = process.env.no_proxy = 'localhost,127.0.0.1';
if (process.argv[2] === '--empty-input-probe') try {
  fs.writeSync(4, JSON.stringify({proxy_configured: ['HTTP_PROXY','HTTPS_PROXY','http_proxy','https_proxy'].every(key => process.env[key] === 'http://host.docker.internal:7890')}));
} catch {}
await import('/app/src/login.mjs');
