# Linux整合竞争检测

日期：2026-11-11。独立执行席使用固定镜像ID `sha256:75195664cd12258c72160cf4121c931ae4176719d6545a9e5f35fabfe2abbc55`，OCI revision为 `2f51beb7c8f9ed7ecb3c91562fe94824192c6990`，实际Go为 `go1.26.11 linux/amd64`。

```text
docker run --rm --network none --env CGO_ENABLED=1 --env GOTOOLCHAIN=local --env GOPROXY=off --env GOSUMDB=off --entrypoint /bin/sh sha256:75195664cd12258c72160cf4121c931ae4176719d6545a9e5f35fabfe2abbc55 -c "go version && go test -race -count=1 -timeout=180s -run '^(TestBinding|TestBPS)' ./internal/bpsbinding ./sdk/cliproxy/auth ./internal/runtime/executor ./internal/runtime/executor/helps ./internal/config ./internal/api ./internal/api/handlers/management"
```

exit0，耗时66.592秒；七个指定包全部通过，无race报告，[原始日志](linux-race.log)。覆盖整合后的最小绑定共享状态、配置热更新、上传取消、公开HTTP两轮和Linux文件读写；这是新BPS路径的定向检测，不是全仓race。

没有安装依赖、联网或真实账号请求。供体全包三项旧失败不在该命令范围内，保留其独立失败记录，没有据此宣布核心全包通过。
