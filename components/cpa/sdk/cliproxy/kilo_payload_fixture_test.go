package cliproxy

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

var kiloPayloadResponseSequence atomic.Int64

// Independent requests receive distinct upstream IDs, as the production API does.
func kiloPayloadSuccessStream(w http.ResponseWriter, r *http.Request) {
	id := fmt.Sprintf("%012d", kiloPayloadResponseSequence.Add(1))
	recorded := httptest.NewRecorder()
	kiloSuccessStream(recorded, r)
	body := bytes.ReplaceAll(recorded.Body.Bytes(), []byte("resp-kilo"), []byte("resp-"+id))
	body = bytes.ReplaceAll(body, []byte("msg-kilo"), []byte("msg-"+id))
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write(body)
	w.(http.Flusher).Flush()
}
