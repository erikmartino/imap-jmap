package jmap

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/coder/websocket"
)

// HandleWebSocket implements the JMAP WebSocket subprotocol per RFC 8887.
// It upgrades HTTP connections to WebSocket using the "jmap" subprotocol,
// processes JMAP Request objects, WebSocketPushEnable / WebSocketPushDisable
// messages, and sends Response / StateChange objects back to the client.
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade to WebSocket, negotiating "jmap" subprotocol per RFC 8887 Section 4.2.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   []string{"jmap"},
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Verify negotiated subprotocol per RFC 8887 Section 4.2.
	if conn.Subprotocol() != "jmap" {
		conn.Close(websocket.StatusPolicyViolation, "Only the 'jmap' subprotocol is supported")
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Channel to receive StateChange events from broadcaster.
	var pushCh chan *StateChange
	var pushTypes []string // nil = all types; populated by WebSocketPushEnable
	pushEnabled := false

	// Send loop: forward broadcaster state changes to the WebSocket client.
	startPushLoop := func(ch chan *StateChange) {
		go func() {
			for sc := range ch {
				if !pushEnabled {
					continue
				}
				// Filter by requested data types if specified.
				outSC := sc
				if len(pushTypes) > 0 {
					filtered := make(map[string]map[string]string)
					for accountID, types := range sc.Changed {
						filteredTypes := make(map[string]string)
						for typeName, state := range types {
							for _, wanted := range pushTypes {
								if typeName == wanted {
									filteredTypes[typeName] = state
									break
								}
							}
						}
						if len(filteredTypes) > 0 {
							filtered[accountID] = filteredTypes
						}
					}
					if len(filtered) == 0 {
						continue
					}
					outSC = &StateChange{
						Type:    "StateChange",
						Changed: filtered,
					}
				}

				msg, err := json.Marshal(outSC)
				if err != nil {
					continue
				}
				if wErr := conn.Write(ctx, websocket.MessageText, msg); wErr != nil {
					return
				}
			}
		}()
	}

	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			// Connection closed or context cancelled — clean up push subscription.
			if pushCh != nil {
				s.Broadcaster.Unsubscribe(pushCh)
			}
			return
		}

		if msgType != websocket.MessageText {
			// RFC 8887 Section 4.3.1: ignore binary frames.
			continue
		}

		// Decode the incoming message @type.
		var typeProbe struct {
			Type string `json:"@type"`
		}
		if err := json.Unmarshal(data, &typeProbe); err != nil {
			writeWSError(ctx, conn, "", ErrorInvalidJSON, "The message could not be parsed as valid JSON.")
			continue
		}

		switch typeProbe.Type {

		case "WebSocketPushEnable":
			// RFC 8887 Section 4.3.5.2: enable push notifications on this connection.
			var pushEnable struct {
				DataTypes []string `json:"dataTypes"`
			}
			_ = json.Unmarshal(data, &pushEnable)

			// Unsubscribe previous push subscription if any.
			if pushCh != nil {
				s.Broadcaster.Unsubscribe(pushCh)
			}

			pushTypes = pushEnable.DataTypes
			pushEnabled = true
			pushCh = s.Broadcaster.Subscribe()
			startPushLoop(pushCh)

		case "WebSocketPushDisable":
			// RFC 8887 Section 4.3.5.3: disable push notifications.
			pushEnabled = false
			if pushCh != nil {
				s.Broadcaster.Unsubscribe(pushCh)
				pushCh = nil
			}

		default:
			// RFC 8887 Section 4.3.2: treat as a JMAP Request object.
			var req struct {
				RequestID   string       `json:"id"`
				Using       []string     `json:"using"`
				MethodCalls []Invocation `json:"methodCalls"`
			}
			if err := json.Unmarshal(data, &req); err != nil {
				writeWSError(ctx, conn, "", ErrorInvalidJSON, "The request body could not be parsed as a JMAP Request.")
				continue
			}

			// Validate capabilities per RFC 8620 Section 3.1.
			capErr := ""
			for _, capURI := range req.Using {
				if _, ok := s.Session.Capabilities[capURI]; !ok {
					capErr = "Unknown capability: " + capURI
					break
				}
			}
			if capErr != "" {
				writeWSError(ctx, conn, req.RequestID, ErrorUnknownCapability, capErr)
				continue
			}

			var responses []Invocation
			executedMap := make(map[string]Invocation)

			for _, call := range req.MethodCalls {
				resolvedArgs, refErr := s.resolveResultReferences(call.Args, executedMap)
				if refErr != "" {
					respInv := Invocation{
						Name:         "error",
						Args:         MethodErrorArgs(MethodErrorInvalidResultReference, refErr),
						ClientCallID: call.ClientCallID,
					}
					responses = append(responses, respInv)
					executedMap[call.ClientCallID] = respInv
					continue
				}

				handler, ok := s.MethodRegistry.Get(call.Name)
				if !ok {
					respInv := Invocation{
						Name:         "error",
						Args:         MethodErrorArgs(MethodErrorUnknownMethod, "Unknown method: "+call.Name),
						ClientCallID: call.ClientCallID,
					}
					responses = append(responses, respInv)
					executedMap[call.ClientCallID] = respInv
					continue
				}

				respName, respArgs := handler(ctx, resolvedArgs, call.ClientCallID)
				respInv := Invocation{
					Name:         respName,
					Args:         respArgs,
					ClientCallID: call.ClientCallID,
				}
				responses = append(responses, respInv)
				executedMap[call.ClientCallID] = respInv
			}

			// RFC 8887 Section 4.3.3: Response includes @type="Response" and requestId.
			resp := map[string]any{
				"@type":           "Response",
				"requestId":       req.RequestID,
				"methodResponses": responses,
				"sessionState":    s.Session.State,
			}
			msg, _ := json.Marshal(resp)
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				if pushCh != nil {
					s.Broadcaster.Unsubscribe(pushCh)
				}
				return
			}
		}
	}
}

// writeWSError sends a JSON Problem Details error over a WebSocket per RFC 8887 Section 4.3.4.
func writeWSError(ctx context.Context, conn *websocket.Conn, requestID, errType, detail string) {
	msg := map[string]any{
		"@type":     "RequestError",
		"requestId": requestID,
		"type":      errType,
		"status":    400,
		"detail":    detail,
	}
	data, _ := json.Marshal(msg)
	_ = conn.Write(ctx, websocket.MessageText, data)
}
