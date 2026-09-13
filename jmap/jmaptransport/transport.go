// Package jmaptransport implements real-time event delivery and streaming protocols for JMAP.
//
// Specification References:
//   - RFC 8620 §7.1: Server-Sent Events (SSE) Stream (/eventsource)
//   - RFC 8887 §3-5: JMAP Subprotocol for WebSocket
package jmaptransport

import (
	"encoding/json"

	"imap-jmap/jmap/jmappush"
)

type StateChange = jmappush.StateChange

// FilterStateChange filters a StateChange event payload by requested accountID and filter data types per RFC 8620 §7.1.
// @spec RFC8620#7.1-p1-MUST
func FilterStateChange(stateEvt *StateChange, principalAccountID, subject, typesParam string, filterTypes map[string]bool) *StateChange {
	if stateEvt == nil {
		return nil
	}
	filteredChanged := make(map[string]map[string]string)
	for accID, typeMap := range stateEvt.Changed {
		match := (principalAccountID == "") || (accID == principalAccountID) || (subject != "" && accID == subject)
		if !match {
			continue
		}
		targetAccID := accID
		if principalAccountID != "" {
			targetAccID = principalAccountID
		}
		filteredMap := make(map[string]string)
		for tName, token := range typeMap {
			if typesParam == "*" || filterTypes[tName] {
				filteredMap[tName] = token
			}
		}
		if len(filteredMap) > 0 {
			if filteredChanged[targetAccID] == nil {
				filteredChanged[targetAccID] = make(map[string]string)
			}
			for k, v := range filteredMap {
				filteredChanged[targetAccID][k] = v
			}
		}
	}
	if len(filteredChanged) == 0 {
		return nil
	}
	return &StateChange{
		Type:    "StateChange",
		Changed: filteredChanged,
	}
}

// FilterWebSocketPush filters a StateChange event for WebSocket push delivery per RFC 8887 §4.3.5.2.
// @spec RFC8887#4.3.5.2-p1-MUST
func FilterWebSocketPush(sc *StateChange, pushTypes []string) ([]byte, bool) {
	if sc == nil {
		return nil, false
	}
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
			return nil, false
		}
		outSC = &StateChange{
			Type:    "StateChange",
			Changed: filtered,
		}
	}
	msg, err := json.Marshal(outSC)
	if err != nil {
		return nil, false
	}
	return msg, true
}
