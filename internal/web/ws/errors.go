package ws

import "errors"

var (
	ErrClientNotConnected = errors.New("client not connected")
	ErrSendBufferFull     = errors.New("send buffer full")
)
