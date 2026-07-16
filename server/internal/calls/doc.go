// Package calls is the call control plane: room lifecycle, short-lived join
// tokens, the ring state machine (ringing → answered/declined/busy/missed),
// call history, and LiveKit webhook reconciliation. Media never crosses
// this package.
//
// Design: Docs/05-services/rtc-lld.md, Docs/04-api/calls-ptt-api.md.
package calls
