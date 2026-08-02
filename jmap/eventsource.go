package jmap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HandleEventSource handles GET requests to /eventsource per RFC 8620 Section 7.1.
func (s *Server) HandleEventSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	typesParam := r.URL.Query().Get("types")
	if typesParam == "" {
		typesParam = "*"
	}
	closeAfter := r.URL.Query().Get("closeafter")
	if closeAfter == "" {
		closeAfter = "no"
	}

	pingSec := 300
	if pingStr := r.URL.Query().Get("ping"); pingStr != "" {
		if p, err := strconv.Atoi(pingStr); err == nil && p > 0 {
			pingSec = p
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if r.Method == http.MethodHead {
		return
	}

	sub := s.Broadcaster.Subscribe()
	defer s.Broadcaster.Unsubscribe(sub)

	pingTicker := time.NewTicker(time.Duration(pingSec) * time.Second)
	defer pingTicker.Stop()

	// Parse types filter
	filterTypes := make(map[string]bool)
	if typesParam != "*" {
		for _, tStr := range strings.Split(typesParam, ",") {
			filterTypes[strings.TrimSpace(tStr)] = true
		}
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return

		case <-pingTicker.C:
			_, err := fmt.Fprintf(w, ": ping\n\n")
			if err != nil {
				return
			}
			flusher.Flush()

		case stateEvt, ok := <-sub:
			if !ok {
				return
			}

			// Filter event by requested types
			if typesParam != "*" {
				filteredChanged := make(map[string]map[string]string)
				for accID, typeMap := range stateEvt.Changed {
					filteredMap := make(map[string]string)
					for tName, token := range typeMap {
						if filterTypes[tName] {
							filteredMap[tName] = token
						}
					}
					if len(filteredMap) > 0 {
						filteredChanged[accID] = filteredMap
					}
				}
				if len(filteredChanged) == 0 {
					continue
				}
				stateEvt = &StateChange{
					Type:    "StateChange",
					Changed: filteredChanged,
				}
			}

			dataBytes, err := json.Marshal(stateEvt)
			if err != nil {
				continue
			}

			_, err = fmt.Fprintf(w, "event: state\ndata: %s\n\n", string(dataBytes))
			if err != nil {
				return
			}
			flusher.Flush()

			if closeAfter == "state" {
				return
			}
		}
	}
}
