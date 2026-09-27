package imapsmtp

import (
	"context"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapextstore"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmappush"
)

// VacationResponse is a per-account singleton per RFC 8621 Section 8.

func (b *IMAPSMTPBackend) loadVacationLocked(ctx context.Context, accountID string) {
	if b.vacationResponses == nil {
		b.vacationResponses = make(map[string]*jmapmail.VacationResponse)
		b.vacationTime = make(map[string]time.Time)
	}
	if b.vacationResponses[accountID] != nil && b.vacationTime != nil && time.Now().Before(b.vacationTime[accountID]) {
		return
	}
	if loaded, ok, _ := jmapextstore.Load[jmapmail.VacationResponse](ctx, b.extStore, accountID, "vacation"); ok && loaded != nil {
		b.vacationResponses[accountID] = loaded
		b.vacationTime[accountID] = time.Now().Add(defaultExtensionCacheTTL)
		return
	}
	b.vacationTime[accountID] = time.Now().Add(defaultExtensionCacheTTL)
}

func (b *IMAPSMTPBackend) persistVacationLocked(ctx context.Context, accountID string) {
	if b.extStore != nil && b.vacationResponses[accountID] != nil {
		_ = jmapextstore.Save(ctx, b.extStore, accountID, "vacation", *b.vacationResponses[accountID])
	}
}

func (b *IMAPSMTPBackend) VacationResponseState(ctx context.Context) string {
	vr, err := b.GetVacationResponse(ctx)
	if err != nil || vr == nil {
		return "vac-v1:empty"
	}
	return "vac-v1:" + jmappush.ObjectFingerprint(vr)
}

func (b *IMAPSMTPBackend) GetVacationResponse(ctx context.Context) (*jmapmail.VacationResponse, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.vacationMu.Lock()
	defer b.vacationMu.Unlock()
	b.loadVacationLocked(ctx, accountID)
	vr, ok := b.vacationResponses[accountID]
	if !ok {
		vr = &jmapmail.VacationResponse{ID: "singleton", IsEnabled: false}
		b.vacationResponses[accountID] = vr
		b.persistVacationLocked(ctx, accountID)
	}
	copyVR := *vr
	return &copyVR, nil
}

func (b *IMAPSMTPBackend) UpdateVacationResponse(ctx context.Context, patch map[string]any) (*jmapmail.VacationResponse, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.vacationMu.Lock()
	defer b.vacationMu.Unlock()
	b.loadVacationLocked(ctx, accountID)
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
	b.persistVacationLocked(ctx, accountID)
	b.publishStateChange(ctx)
	copyVR := *vr
	return &copyVR, nil
}
