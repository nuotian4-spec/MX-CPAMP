package handlers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/MoeForge/moego/ojson"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type presentationTestError struct{}

func (presentationTestError) Error() string   { return "original dispatch text" }
func (presentationTestError) StatusCode() int { return 404 }
func (presentationTestError) Headers() http.Header {
	return http.Header{"X-Fixture": []string{"preserved"}}
}
func (presentationTestError) PublicErrorBody() string {
	return `{"error":{"code":"model_not_found","type":"invalid_request_error","message":"public text"}}`
}

type directPresentationTestError struct{ presentationTestError }

func (directPresentationTestError) DirectResponse() bool { return true }
func (directPresentationTestError) ResponseBody() []byte { return []byte("direct body") }

func TestPublicExecutionErrorPresentation(t *testing.T) {
	original := presentationTestError{}
	msg := executionErrorMessage(original)
	if msg.StatusCode != 404 || msg.Addon.Get("X-Fixture") != "preserved" || msg.Error.Error() != original.PublicErrorBody() || errors.Unwrap(msg.Error) != original {
		t.Fatal("public presentation lost status, headers, body, or original cause")
	}
	if original.Error() != "original dispatch text" {
		t.Fatal("public presentation changed dispatch text")
	}
	direct := directPresentationTestError{}
	directMsg := executionErrorMessage(direct)
	if !directMsg.DirectResponse || string(directMsg.Body) != "direct body" || directMsg.Error != direct {
		t.Fatal("public presentation changed direct-response precedence")
	}
	terminal := coreauth.NewTerminalAuthError(&coreauth.Error{HTTPStatus: 401, Message: "terminal"}, original)
	terminalMsg := executionErrorMessage(terminal)
	body := BuildErrorResponseBodyWithError(terminalMsg.StatusCode, terminalMsg.Error.Error(), terminalMsg.Error)
	if ojson.GetBytes(body, "error.code").String() != "upstream_authentication_required" || ojson.GetBytes(body, "error.retryable").Bool() {
		t.Fatal("public presentation changed terminal-auth precedence")
	}
}
