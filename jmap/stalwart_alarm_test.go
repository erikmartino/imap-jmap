package jmap_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/managesieve"
	"imap-jmap/jmap/nextcloud"
	"imap-jmap/jmap/spectest"
)

func authedRequestAs(t *testing.T, method, url string, body io.Reader, user string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("NewRequest(%s %s) failed: %v", method, url, err)
	}
	req.SetBasicAuth(user, user)
	return req
}

func basicAuthHeaderFor(user string) http.Header {
	return http.Header{"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+user))}}
}

// TestStalwart_CalendarAlarms translates Stalwart's calendar alarm integration
// test suite (tests/src/jmap/calendar/alarm.rs) to Go.
func TestStalwart_CalendarAlarms(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "8", spectest.MUST,
		"A CalendarAlert object represents an alert on a calendar event that has triggered.")

	user := "jdoe@example.com"
	gwBackend, _ := imapsmtp.NewEmbeddedBackend(user)
	_, cal, contacts, fb, principals, cleanupNC := nextcloud.NewEmbeddedBackend(user)
	defer cleanupNC()
	_, sieve, cleanupSieve := managesieve.NewEmbeddedBackend(user)
	defer cleanupSieve()
	imap := jmap.NewMemoryIMAPAccessBackend()
	memAuth := jmap.NewMemoryAuthBackend()
	memAuth.SetDisableSeeding(true)

	srv := jmap.NewServer(nil,
		jmap.WithMailBackend(gwBackend),
		jmap.WithBlobBackend(gwBackend),
		jmap.WithFileNodeBackend(fb),
		jmap.WithCalendarsBackend(cal),
		jmap.WithContactsBackend(contacts),
		jmap.WithPrincipalsBackend(principals),
		jmap.WithSieveBackend(sieve),
		jmap.WithIMAPAccessBackend(imap),
		jmap.WithAuthBackend(memAuth),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{
		jmap.CoreCapabilityURI,
		jmap.CalendarsCapabilityURI,
		jmap.WebSocketCapabilityURI,
	}

	accountID := jmap.AccountIDForSubject(user)

	// 1. Create test calendar
	calResp := postJMAPAs(t, ts.URL, user, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"name": "Alarming Calendar",
				},
			},
		}, "c_cal"},
	})
	c1Map, _ := calResp.MethodResponses[0].Args["created"].(map[string]any)
	calendarID := c1Map["c1"].(map[string]any)["id"].(string)

	// 2. Connect to EventSource
	esReq := authedRequestAs(t, "GET", ts.URL+"/eventsource", nil, user)
	esClient := &http.Client{}
	esHTTPResp, err := esClient.Do(esReq)
	if err != nil {
		t.Fatalf("GET /eventsource failed: %v", err)
	}
	defer esHTTPResp.Body.Close()

	esAlertsCh := make(chan jmap.CalendarAlert, 10)
	go func() {
		scanner := bufio.NewScanner(esHTTPResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var probe struct {
				Type string `json:"@type"`
			}
			if err := json.Unmarshal([]byte(data), &probe); err == nil && probe.Type == "CalendarAlert" {
				var alert jmap.CalendarAlert
				if err := json.Unmarshal([]byte(data), &alert); err == nil {
					esAlertsCh <- alert
				}
			}
		}
	}()

	// 3. Connect to WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/websocket"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
		HTTPHeader:   basicAuthHeaderFor(user),
	})
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer wsConn.Close(websocket.StatusNormalClosure, "")

	// Enable push on WebSocket
	enableMsg, _ := json.Marshal(map[string]any{
		"@type":     "WebSocketPushEnable",
		"dataTypes": nil,
	})
	if err := wsConn.Write(ctx, websocket.MessageText, enableMsg); err != nil {
		t.Fatalf("failed to enable push on ws: %v", err)
	}

	wsAlertsCh := make(chan jmap.CalendarAlert, 10)
	go func() {
		for {
			_, msgBytes, err := wsConn.Read(ctx)
			if err != nil {
				return
			}
			var probe struct {
				Type string `json:"@type"`
			}
			if err := json.Unmarshal(msgBytes, &probe); err == nil && probe.Type == "CalendarAlert" {
				var alert jmap.CalendarAlert
				if err := json.Unmarshal(msgBytes, &alert); err == nil {
					wsAlertsCh <- alert
				}
			}
		}
	}()

	// Small pause to ensure SSE and WS connections are active in broadcaster
	time.Sleep(100 * time.Millisecond)

	// 4. Create test event starting 5 seconds in the future
	startStr := time.Now().Add(5 * time.Second).UTC().Format("2006-01-02T15:04:05")
	eventPayload := map[string]any{
		"@type":       "Event",
		"calendarIds": map[string]bool{calendarID: true},
		"description": "What mirror where?!",
		"timeZone":    "Etc/UTC",
		"start":       startStr,
		"title":       "See the pretty girl in that mirror there",
		"alerts": map[string]any{
			"k1": map[string]any{
				"@type": "Alert",
				"trigger": map[string]any{
					"@type":  "OffsetTrigger",
					"offset": "-PT2S",
				},
				"action": "display",
			},
			"k2": map[string]any{
				"@type": "Alert",
				"trigger": map[string]any{
					"@type":  "OffsetTrigger",
					"offset": "-PT4S",
				},
				"action": "display",
			},
		},
		"locations": map[string]any{
			"0b7168ae-ed3e-5eae-9540-89ba3a469b16": map[string]any{
				"name":  "West Side",
				"@type": "Location",
			},
		},
		"uid":      "2371c2d9-a136-43b0-bba3-f6ab249ad46e",
		"duration": "P1D",
	}

	evResp := postJMAPAs(t, ts.URL, user, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": eventPayload,
			},
		}, "ce1"},
	})
	e1Map, _ := evResp.MethodResponses[0].Args["created"].(map[string]any)
	if e1Map == nil || e1Map["e1"] == nil {
		t.Fatalf("failed to create event: %+v", evResp.MethodResponses[0].Args)
	}
	eventID := e1Map["e1"].(map[string]any)["id"].(string)

	// 5. Wait for alarm notifications (up to 7 seconds)
	var esEvents []jmap.CalendarAlert
	var wsEvents []jmap.CalendarAlert

	timeoutTimer := time.After(7 * time.Second)
waitLoop:
	for len(esEvents) < 2 || len(wsEvents) < 2 {
		select {
		case alert := <-esAlertsCh:
			esEvents = append(esEvents, alert)
		case alert := <-wsAlertsCh:
			wsEvents = append(wsEvents, alert)
		case <-timeoutTimer:
			break waitLoop
		}
	}

	expectedAlerts := []jmap.CalendarAlert{
		{
			Type:            "CalendarAlert",
			AccountID:       accountID,
			CalendarEventID: eventID,
			UID:             "2371c2d9-a136-43b0-bba3-f6ab249ad46e",
			RecurrenceID:    nil,
			AlertID:         "k2",
		},
		{
			Type:            "CalendarAlert",
			AccountID:       accountID,
			CalendarEventID: eventID,
			UID:             "2371c2d9-a136-43b0-bba3-f6ab249ad46e",
			RecurrenceID:    nil,
			AlertID:         "k1",
		},
	}

	if !reflect.DeepEqual(esEvents, expectedAlerts) {
		t.Fatalf("EventSource alarms do not match:\nexpected: %+v\ngot:      %+v", expectedAlerts, esEvents)
	}
	if !reflect.DeepEqual(wsEvents, expectedAlerts) {
		t.Fatalf("WebSocket alarms do not match:\nexpected: %+v\ngot:      %+v", expectedAlerts, wsEvents)
	}
}
