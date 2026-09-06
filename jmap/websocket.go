package jmap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coder/websocket"
)

// HandleWebSocket implements the JMAP WebSocket subprotocol per RFC 8887.
// It upgrades HTTP connections to WebSocket using the "jmap" subprotocol,
// processes JMAP Request objects, WebSocketPushEnable / WebSocketPushDisable
// messages, and sends Response / StateChange objects back to the client.
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   []string{"jmap"},
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		slog.Debug("WebSocket handshake failed", "remote", r.RemoteAddr, "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if conn.Subprotocol() != "jmap" {
		slog.Warn("WebSocket subprotocol mismatch", "remote", r.RemoteAddr, "negotiated", conn.Subprotocol())
		conn.Close(websocket.StatusPolicyViolation, "Only the 'jmap' subprotocol is supported")
		return
	}

	user, _ := SubjectFromContext(r.Context())
	accountID, _ := AccountIDFromContext(r.Context())
	slog.Info("WebSocket client connected", "remote", r.RemoteAddr, "user", user, "accountId", accountID, "subprotocol", conn.Subprotocol())

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
					for acctID, types := range sc.Changed {
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
							filtered[acctID] = filteredTypes
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
				slog.Debug("WebSocket push StateChange", "remote", r.RemoteAddr, "user", user, "payload", string(msg))
				if wErr := conn.Write(ctx, websocket.MessageText, msg); wErr != nil {
					slog.Debug("WebSocket push write error", "remote", r.RemoteAddr, "error", wErr)
					return
				}
			}
		}()
	}

	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			slog.Info("WebSocket client disconnected", "remote", r.RemoteAddr, "user", user, "error", err)
			if pushCh != nil {
				s.Broadcaster.Unsubscribe(pushCh)
			}
			return
		}

		if msgType != websocket.MessageText {
			// RFC 8887 Section 4.3.1: ignore binary frames.
			continue
		}

		limits := s.coreLimits(r)
		if limits.MaxSizeRequest > 0 && uint64(len(data)) > limits.MaxSizeRequest {
			writeWSError(ctx, conn, "", ErrorLimit, "The message size exceeded maxSizeRequest", "maxSizeRequest")
			continue
		}

		// Decode the incoming message @type.
		var typeProbe struct {
			Type string `json:"@type"`
		}
		if err := json.Unmarshal(data, &typeProbe); err != nil {
			slog.Debug("WebSocket invalid JSON message", "remote", r.RemoteAddr, "error", err)
			writeWSError(ctx, conn, "", ErrorNotJSON, "The message could not be parsed as valid JSON.")
			continue
		}

		switch typeProbe.Type {

		case "WebSocketPushEnable":
			// RFC 8887 Section 4.3.5.2: enable push notifications on this connection.
			var pushEnable struct {
				DataTypes []string `json:"dataTypes"`
			}
			_ = json.Unmarshal(data, &pushEnable)
			slog.Debug("WebSocket push enabled", "remote", r.RemoteAddr, "user", user, "dataTypes", pushEnable.DataTypes)

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
			slog.Debug("WebSocket push disabled", "remote", r.RemoteAddr, "user", user)
			pushEnabled = false
			if pushCh != nil {
				s.Broadcaster.Unsubscribe(pushCh)
				pushCh = nil
			}

		default:
			// RFC 8887 Section 4.3.2: treat as a JMAP Request object.
			slog.Debug("WebSocket JMAP Request received", "remote", r.RemoteAddr, "user", user, "payload", string(data))
			var req struct {
				RequestID   string            `json:"id"`
				Using       []string          `json:"using"`
				MethodCalls []Invocation      `json:"methodCalls"`
				CreatedIds  map[string]string `json:"createdIds,omitempty"`
			}
			if err := json.Unmarshal(data, &req); err != nil {
				writeWSError(ctx, conn, "", ErrorNotRequest, "The request body could not be parsed as a JMAP Request.")
				continue
			}

			if limits.MaxCallsInRequest > 0 && uint64(len(req.MethodCalls)) > limits.MaxCallsInRequest {
				writeWSError(ctx, conn, req.RequestID, ErrorLimit, "The number of method calls exceeded maxCallsInRequest", "maxCallsInRequest")
				continue
			}

			// Validate capabilities per RFC 8620 Section 3.1.
			capErr := ""
			for _, capURI := range req.Using {
				if !s.capabilitySupported(capURI) {
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

			refs := NewCreationRefs(req.CreatedIds)
			reqCtx := WithCreationRefs(ctx, refs)
			reqCtx = WithCoreLimits(reqCtx, limits)
			var calCap CalendarsCapability
			if s.Session != nil && s.Session.Capabilities != nil {
				if c, ok := s.Session.Capabilities[CalendarsCapabilityURI].(CalendarsCapability); ok {
					calCap = c
				}
			}
			reqCtx = WithCalendarsCapability(reqCtx, calCap)

			for _, call := range req.MethodCalls {
				resolvedArgs, refErrType, refErr := s.resolveResultReferences(call.Args, executedMap)
				if refErr != "" {
					respInv := Invocation{
						Name:         "error",
						Args:         MethodErrorArgs(refErrType, refErr),
						ClientCallID: call.ClientCallID,
					}
					responses = append(responses, respInv)
					executedMap[call.ClientCallID] = respInv
					continue
				}

				// RFC 8620 Section 2.2 & 5.1: maxObjectsInGet enforcement
				if strings.HasSuffix(call.Name, "/get") {
					if rawIDs, hasIDs := resolvedArgs["ids"]; hasIDs && rawIDs != nil {
						if idsSlice, ok := rawIDs.([]any); ok {
							if limits.MaxObjectsInGet > 0 && uint64(len(idsSlice)) > limits.MaxObjectsInGet {
								respInv := Invocation{
									Name:         "error",
									Args:         MethodErrorArgs(MethodErrorRequestTooLarge, fmt.Sprintf("Number of requested ids (%d) exceeds maxObjectsInGet (%d)", len(idsSlice), limits.MaxObjectsInGet)),
									ClientCallID: call.ClientCallID,
								}
								responses = append(responses, respInv)
								executedMap[call.ClientCallID] = respInv
								continue
							}
						}
					}
				}

				// RFC 8620 Section 2.2 & 5.3: maxObjectsInSet enforcement
				if strings.HasSuffix(call.Name, "/set") {
					var setCount uint64
					if c, ok := resolvedArgs["create"].(map[string]any); ok {
						setCount += uint64(len(c))
					}
					if u, ok := resolvedArgs["update"].(map[string]any); ok {
						setCount += uint64(len(u))
					}
					if d, ok := resolvedArgs["destroy"].([]any); ok {
						setCount += uint64(len(d))
					}
					if limits.MaxObjectsInSet > 0 && setCount > limits.MaxObjectsInSet {
						respInv := Invocation{
							Name:         "error",
							Args:         MethodErrorArgs(MethodErrorRequestTooLarge, fmt.Sprintf("Total objects in set (%d) exceeds maxObjectsInSet (%d)", setCount, limits.MaxObjectsInSet)),
							ClientCallID: call.ClientCallID,
						}
						responses = append(responses, respInv)
						executedMap[call.ClientCallID] = respInv
						continue
					}
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

				respName, respArgs := handler(reqCtx, resolvedArgs, call.ClientCallID)
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
			if req.CreatedIds != nil {
				resp["createdIds"] = refs.Snapshot()
			}
			msg, _ := json.Marshal(resp)
			slog.Debug("WebSocket JMAP Response sent", "remote", r.RemoteAddr, "user", user, "payload", string(msg))
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
func writeWSError(ctx context.Context, conn *websocket.Conn, requestID, errType, detail string, limit ...string) {
	status := 400
	if len(limit) > 0 && limit[0] == "maxSizeRequest" {
		status = 413
	}
	msg := map[string]any{
		"@type":     "RequestError",
		"requestId": requestID,
		"type":      errType,
		"status":    status,
		"detail":    detail,
	}
	if len(limit) > 0 && limit[0] != "" {
		msg["limit"] = limit[0]
	}
	data, _ := json.Marshal(msg)
	_ = conn.Write(ctx, websocket.MessageText, data)
}
