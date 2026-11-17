package handlers

import (
	"context"
	"errors"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// ExecutionErrorMessageForRequest applies the same scoped presentation to
// dedicated protocol adapters after their execution has finished.
func ExecutionErrorMessageForRequest(ctx context.Context, err error, streamStarted bool) *interfaces.ErrorMessage {
	return executionErrorMessageForRequest(ctx, err, streamStarted)
}

func executionPreflightErrorMessage(ctx context.Context, msg *interfaces.ErrorMessage) *interfaces.ErrorMessage {
	if msg == nil || channelscope.From(ctx) != "kilo" {
		return msg
	}
	err := msg.Error
	if msg.StatusCode >= 400 && msg.StatusCode < 500 {
		err = coreauth.WithCause(&coreauth.Error{Code: coreauth.ErrorCodeRequestScoped, HTTPStatus: msg.StatusCode}, err)
	}
	safe := executionErrorMessageForRequest(ctx, err, false)
	safe.StatusCode = msg.StatusCode
	return safe
}

// Kilo is only selectable through its dedicated channel. Apply presentation
// after the auth manager has finished all scheduling and state transitions.
func executionErrorMessageForRequest(ctx context.Context, err error, streamStarted bool) *interfaces.ErrorMessage {
	msg := executionErrorMessage(err)
	if channelscope.From(ctx) != "kilo" {
		return msg
	}
	presentation := helps.PresentKiloError(err)
	requestID := logging.GetRequestID(ctx)
	msg.Error = publicExecutionError{message: presentation.Body(requestID), cause: err, requestID: requestID}
	msg.DirectResponse, msg.Body, msg.Headers, msg.Addon = false, nil, nil, nil
	var alreadyPresented interface{ RequestID() string }
	if errors.As(err, &alreadyPresented) && alreadyPresented.RequestID() == requestID {
		return msg
	}
	log.WithFields(log.Fields{
		"request_id": requestID, "provider": "kilo", "status": msg.StatusCode,
		"upstream_status": presentation.UpstreamStatus, "code": presentation.InternalCode,
		"error_type": presentation.InternalType, "public_code": presentation.Code,
		"stream_started": streamStarted,
	}).Warn("upstream request failed")
	return msg
}
