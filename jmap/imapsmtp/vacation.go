package imapsmtp

import (
	"context"
	"fmt"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapmail"
)

// VacationResponse is a per-account singleton per RFC 8621 Section 8.

func (b *IMAPSMTPBackend) VacationResponseState(ctx context.Context) string {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.vacationMu.RLock()
	defer b.vacationMu.RUnlock()
	st := b.vacationState[accountID]
	if st == 0 {
		return "1"
	}
	return fmt.Sprintf("%d", st)
}

func (b *IMAPSMTPBackend) GetVacationResponse(ctx context.Context) (*jmapmail.VacationResponse, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.vacationMu.Lock()
	defer b.vacationMu.Unlock()
	if b.vacationResponses == nil {
		b.vacationResponses = make(map[string]*jmapmail.VacationResponse)
	}
	vr, ok := b.vacationResponses[accountID]
	if !ok {
		vr = &jmapmail.VacationResponse{ID: "singleton", IsEnabled: false}
		b.vacationResponses[accountID] = vr
	}
	copyVR := *vr
	return &copyVR, nil
}

func (b *IMAPSMTPBackend) UpdateVacationResponse(ctx context.Context, patch map[string]any) (*jmapmail.VacationResponse, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.vacationMu.Lock()
	defer b.vacationMu.Unlock()
	if b.vacationResponses == nil {
		b.vacationResponses = make(map[string]*jmapmail.VacationResponse)
	}
	if b.vacationState == nil {
		b.vacationState = make(map[string]uint64)
	}
	vr, ok := b.vacationResponses[accountID]
	if !ok {
		vr = &jmapmail.VacationResponse{ID: "singleton", IsEnabled: false}
		b.vacationResponses[accountID] = vr
	}
	for k, v := range patch {
		switch k {
		case "isEnabled":
			if bVal, ok := v.(bool); ok {
				vr.IsEnabled = bVal
			}
		case "fromDate":
			if v == nil {
				vr.FromDate = nil
			} else if s, ok := v.(string); ok {
				vr.FromDate = &s
			}
		case "toDate":
			if v == nil {
				vr.ToDate = nil
			} else if s, ok := v.(string); ok {
				vr.ToDate = &s
			}
		case "subject":
			if v == nil {
				vr.Subject = nil
			} else if s, ok := v.(string); ok {
				vr.Subject = &s
			}
		case "textBody":
			if v == nil {
				vr.TextBody = nil
			} else if s, ok := v.(string); ok {
				vr.TextBody = &s
			}
		case "htmlBody":
			if v == nil {
				vr.HTMLBody = nil
			} else if s, ok := v.(string); ok {
				vr.HTMLBody = &s
			}
		}
	}
	if b.vacationState[accountID] == 0 {
		b.vacationState[accountID] = 1
	}
	b.vacationState[accountID]++
	b.publishStateChange(ctx)
	copyVR := *vr
	return &copyVR, nil
}
