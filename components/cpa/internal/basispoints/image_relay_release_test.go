package basispoints

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReleaseRelayKeepsLiveImageCapability(t *testing.T) {
	relay, err := newTestImageRelay(t, "https://node-a.example.invalid")
	require.NoError(t, err)
	require.Zero(t, relay.LiveResources())
	_, err = relay.Rewrite(relayTestRequest(t, relayTestPNG(t)), "account:201/key:101/thread:release")
	require.NoError(t, err)
	require.Equal(t, int64(1), relay.LiveResources(), "HTTP结束后图片能力仍依赖原进程")
	require.NoError(t, relay.Close())
	require.Zero(t, relay.LiveResources(), "已关闭并删除文件的能力不应继续计为存活资源")
}
