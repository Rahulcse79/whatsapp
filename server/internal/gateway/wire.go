package gateway

// Interim JSON handshake frames. These stand in for the binary protobuf
// frames of websocket-protocol.md until the delivery path (T0.12) wires the
// generated wsv1 types. Keeping them isolated here makes that swap a
// contained change — nothing outside gateway depends on this codec.

// frameType discriminates a control frame.
type frameType string

const (
	frameHello    frameType = "hello"
	frameHelloAck frameType = "hello_ack"
	frameError    frameType = "error"
)

// helloFrame is the client's opening frame. The resume_token / last-cursor
// fields arrive with the session-resume path (T0.13).
type helloFrame struct {
	Type      frameType `json:"type"`
	AccessJWT string    `json:"access_jwt"`
}

// helloAckFrame acknowledges a successful handshake.
type helloAckFrame struct {
	Type         frameType `json:"type"`
	SessionID    string    `json:"session_id"`
	ServerTimeMS int64     `json:"server_time_ms"`
	// ResumeToken rotation and inbox replay arrive with the resume path (T0.13).
}

// errorFrame carries a pre-close error to the client (mirrors the uniform
// error envelope shape).
type errorFrame struct {
	Type    frameType `json:"type"`
	Code    string    `json:"code"`
	Message string    `json:"message"`
}
