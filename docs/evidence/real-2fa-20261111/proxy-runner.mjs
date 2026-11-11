// 仅供本地真实登录验收；Tasks 会过滤父进程的代理环境。
process.env.HTTP_PROXY = process.env.HTTPS_PROXY = 'http://host.docker.internal:7890';
process.env.http_proxy = process.env.https_proxy = process.env.HTTPS_PROXY;
process.env.NO_PROXY = process.env.no_proxy = 'localhost,127.0.0.1';
await import('/app/src/login.mjs');
