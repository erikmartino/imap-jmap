package memory

import (
	"context"
	"fmt"
	"imap-jmap/jmap"
	"sort"
	"strings"
	"time"
)

func (mb *MemoryBackend) IdentityState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.identityState.State()
}

// IdentityChanges returns created, updated, and destroyed Identities since sinceState.
func (mb *MemoryBackend) IdentityChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.identityState.Changes(sinceState, maxChanges)
}

// SubmissionState returns current change state token for EmailSubmission resources.
func (mb *MemoryBackend) SubmissionState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.submissionState.State()
}

// SubmissionChanges returns created, updated, and destroyed EmailSubmissions since sinceState.
func (mb *MemoryBackend) SubmissionChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.submissionState.Changes(sinceState, maxChanges)
}

// QuotaState returns current change state token for Quota resources.
func (mb *MemoryBackend) QuotaState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.quotaState.State()
}

// QuotaChanges returns created, updated, and destroyed Quotas since sinceState.
func (mb *MemoryBackend) QuotaChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.quotaState.Changes(sinceState, maxChanges)
}

// GetMailboxes retrieves requested mailboxes by ID.

func (mb *MemoryBackend) GetIdentities(ctx context.Context) ([]*jmap.Identity, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Identity, 0, len(us.identities))
	for _, item := range us.identities {
		list = append(list, item)
	}
	return list, nil
}

// CreateIdentity creates a new Identity (RFC 8621 Section 6.3).
func (mb *MemoryBackend) CreateIdentity(ctx context.Context, identity *jmap.Identity) (*jmap.Identity, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	if identity.Email == "" {
		mb.mu.Unlock()
		return nil, fmt.Errorf("email is required")
	}
	if identity.ID == "" {
		identity.ID = mb.nextID("identity")
	}
	identity.MayDelete = true
	us.identities[identity.ID] = identity
	mb.mu.Unlock()

	mb.recordChange(ctx, us.identityState, identity.ID, "create", "Identity")
	return identity, nil
}

// UpdateIdentity applies a partial patch to an existing Identity, preserving unaddressed fields.
func (mb *MemoryBackend) UpdateIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Identity, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	identity, ok := us.identities[id]
	if !ok {
		mb.mu.Unlock()
		return nil, fmt.Errorf("identity %s: %w", id, jmap.ErrNotFound)
	}

	// The "email" property is server-set and immutable per RFC 8621 Section 6.
	if _, present := patch["email"]; present {
		mb.mu.Unlock()
		return nil, fmt.Errorf("email is immutable")
	}
	if v, ok := patch["name"].(string); ok {
		identity.Name = v
	}
	if v, ok := patch["textSignature"].(string); ok {
		identity.TextSignature = v
	}
	if v, ok := patch["htmlSignature"].(string); ok {
		identity.HTMLSignature = v
	}
	if v, present := patch["replyTo"]; present {
		if v == nil {
			identity.ReplyTo = nil
		} else if arr, ok := v.([]any); ok {
			var addrs []jmap.EmailAddress
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					name, _ := m["name"].(string)
					email, _ := m["email"].(string)
					addrs = append(addrs, jmap.EmailAddress{Name: name, Email: email})
				}
			}
			identity.ReplyTo = addrs
		}
	}
	if v, present := patch["bcc"]; present {
		if v == nil {
			identity.BCC = nil
		} else if arr, ok := v.([]any); ok {
			var addrs []jmap.EmailAddress
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					name, _ := m["name"].(string)
					email, _ := m["email"].(string)
					addrs = append(addrs, jmap.EmailAddress{Name: name, Email: email})
				}
			}
			identity.BCC = addrs
		}
	}
	mb.mu.Unlock()

	mb.recordChange(ctx, us.identityState, id, "update", "Identity")
	return identity, nil
}

// DeleteIdentity removes an Identity.
func (mb *MemoryBackend) DeleteIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	if _, ok := us.identities[id]; !ok {
		mb.mu.Unlock()
		return false, nil
	}
	delete(us.identities, id)
	mb.mu.Unlock()

	mb.recordChange(ctx, us.identityState, id, "destroy", "Identity")
	return true, nil
}

// VacationResponseState returns the change token for the VacationResponse singleton (RFC 8621 Section 8).
func (mb *MemoryBackend) VacationResponseState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.getStoreLocked(ctx).vacationState.State()
}

// GetVacationResponse returns the per-account VacationResponse singleton (id "singleton").
func (mb *MemoryBackend) GetVacationResponse(ctx context.Context) (*jmap.VacationResponse, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	cp := *us.vacationResponse
	return &cp, nil
}

// UpdateVacationResponse applies a partial patch to the VacationResponse singleton, leaving
// unaddressed properties untouched (RFC 8621 Section 8.2 / RFC 8620 Section 5.3).
func (mb *MemoryBackend) UpdateVacationResponse(ctx context.Context, patch map[string]any) (*jmap.VacationResponse, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	vr := us.vacationResponse

	setStrPtr := func(v any, dst **string) {
		if v == nil {
			*dst = nil
			return
		}
		if s, ok := v.(string); ok {
			*dst = &s
		}
	}
	for k, v := range patch {
		switch k {
		case "id":
			// Server-set; ignore.
		case "isEnabled":
			if b, ok := v.(bool); ok {
				vr.IsEnabled = b
			}
		case "fromDate":
			setStrPtr(v, &vr.FromDate)
		case "toDate":
			setStrPtr(v, &vr.ToDate)
		case "subject":
			setStrPtr(v, &vr.Subject)
		case "textBody":
			setStrPtr(v, &vr.TextBody)
		case "htmlBody":
			setStrPtr(v, &vr.HTMLBody)
		default:
			mb.mu.Unlock()
			return nil, fmt.Errorf("unknown VacationResponse property: %s", k)
		}
	}
	cp := *vr
	mb.mu.Unlock()

	mb.recordChange(ctx, us.vacationState, "singleton", "update", "VacationResponse")
	return &cp, nil
}

// CreateSubmission creates an EmailSubmission.
func (mb *MemoryBackend) CreateSubmission(ctx context.Context, sub *jmap.EmailSubmission) (*jmap.EmailSubmission, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if sub.ID == "" {
		sub.ID = mb.nextID("sub")
	}
	if sub.SendAt == "" {
		sub.SendAt = time.Now().UTC().Format(time.RFC3339)
	}
	// RFC 8621 Section 7.1: threadId is server-set and must match the referenced email.
	if sub.ThreadID == "" {
		if em, ok := us.emails[sub.EmailID]; ok {
			sub.ThreadID = em.ThreadID
		}
	}
	if sub.UndoStatus == "" {
		if t, err := time.Parse(time.RFC3339, sub.SendAt); err == nil && t.After(time.Now().UTC().Add(1*time.Second)) {
			sub.UndoStatus = "pending"
		} else {
			sub.UndoStatus = "final"
		}
	}
	if sub.DeliveryStatus == nil {
		sub.DeliveryStatus = make(map[string]jmap.DeliveryStatus)
	}

	us.submissions[sub.ID] = sub
	mb.recordChange(ctx, us.submissionState, sub.ID, "create", "EmailSubmission")
	return sub, nil
}

// UpdateSubmission applies a patch to an EmailSubmission per RFC 8621 Section 7.5.
// EmailSubmission objects are immutable except for updating undoStatus to "canceled" when pending.
func (mb *MemoryBackend) UpdateSubmission(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.EmailSubmission, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	sub, ok := us.submissions[id]
	if !ok {
		return nil, fmt.Errorf("notFound: submission not found")
	}

	for k, v := range patch {
		cleanK := strings.TrimPrefix(k, "/")
		switch cleanK {
		case "undoStatus":
			newStatus, _ := v.(string)
			if newStatus != "canceled" {
				return nil, fmt.Errorf("invalidProperties: undoStatus can only be updated to canceled")
			}
			if sub.UndoStatus == "final" {
				return nil, fmt.Errorf("cannotCancel: submission is already final")
			}
			if sub.UndoStatus == "canceled" {
				return nil, fmt.Errorf("alreadyCanceled: submission is already canceled")
			}
			sub.UndoStatus = "canceled"
			for rcpt, ds := range sub.DeliveryStatus {
				if ds.Delivered == "queued" || ds.Delivered == "pending" {
					ds.Delivered = "no"
					ds.SmtpReply = "canceled by user"
					sub.DeliveryStatus[rcpt] = ds
				}
			}
		default:
			return nil, fmt.Errorf("invalidProperties: EmailSubmission property %q cannot be updated", k)
		}
	}

	mb.recordChange(ctx, us.submissionState, sub.ID, "update", "EmailSubmission")
	return sub, nil
}

// DeleteSubmission deletes an EmailSubmission.
func (mb *MemoryBackend) DeleteSubmission(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if _, ok := us.submissions[id]; !ok {
		return false, nil
	}
	delete(us.submissions, id)
	mb.recordChange(ctx, us.submissionState, id, "destroy", "EmailSubmission")
	return true, nil
}

// GetSubmissions retrieves EmailSubmissions by ID.
func (mb *MemoryBackend) GetSubmissions(ctx context.Context, ids []jmap.Id) ([]*jmap.EmailSubmission, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.EmailSubmission
	var notFound []jmap.Id

	for _, id := range ids {
		if sub, ok := us.submissions[id]; ok {
			list = append(list, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllSubmissions retrieves all EmailSubmissions.
func (mb *MemoryBackend) GetAllSubmissions(ctx context.Context) ([]*jmap.EmailSubmission, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.EmailSubmission, 0, len(us.submissions))
	for _, sub := range us.submissions {
		list = append(list, sub)
	}
	return list, nil
}

// LookupBlobReferences implements jmap.BlobReferenceBackend per RFC 9404 Section 4.3: for a
// blob id, which objects of the requested JMAP data types reference it.
func (mb *MemoryBackend) LookupBlobReferences(ctx context.Context, typeNames []string, blobID jmap.Id) (map[string][]jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	matched := make(map[string][]jmap.Id)
	for _, tn := range typeNames {
		switch tn {
		case "Email":
			for _, em := range us.emails {
				if em.BlobID == blobID || emailReferencesBlob(em, blobID) {
					matched["Email"] = append(matched["Email"], em.ID)
				}
			}
		case "Thread":
			for _, th := range us.threads {
				for _, emailID := range th.EmailIDs {
					if em, ok := us.emails[emailID]; ok && (em.BlobID == blobID || emailReferencesBlob(em, blobID)) {
						matched["Thread"] = append(matched["Thread"], th.ID)
						break
					}
				}
			}
		case "Mailbox":
			// No Mailbox property currently references blobs, so nothing can match.
		}
	}
	return matched, nil
}

// emailReferencesBlob reports whether an email references a blob via its attachments.
func emailReferencesBlob(em *jmap.Email, blobID jmap.Id) bool {
	for _, att := range em.Attachments {
		if att.BlobID != nil && *att.BlobID == blobID {
			return true
		}
	}
	return false
}

// MatchSubmissionFilter checks if an EmailSubmission matches a FilterCondition or FilterOperator.
func MatchSubmissionFilter(sub *jmap.EmailSubmission, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}

	// FilterOperator: operator (AND/OR/NOT) + conditions
	if opRaw, ok := filter["operator"].(string); ok {
		condsRaw, ok := filter["conditions"].([]any)
		if !ok {
			return true
		}
		op := strings.ToUpper(opRaw)
		switch op {
		case "AND":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if !MatchSubmissionFilter(sub, condMap) {
						return false
					}
				}
			}
			return true
		case "OR":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if MatchSubmissionFilter(sub, condMap) {
						return true
					}
				}
			}
			return len(condsRaw) == 0
		case "NOT":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if MatchSubmissionFilter(sub, condMap) {
						return false
					}
				}
			}
			return true
		}
	}

	identityFilter := submissionIDFilter(filter, "identityIds")
	emailFilter := submissionIDFilter(filter, "emailIds")
	threadFilter := submissionIDFilter(filter, "threadIds")
	undoStatus, _ := filter["undoStatus"].(string)
	before, _ := filter["before"].(string)
	after, _ := filter["after"].(string)

	if len(identityFilter) > 0 && !identityFilter[sub.IdentityID] {
		return false
	}
	if len(emailFilter) > 0 && !emailFilter[sub.EmailID] {
		return false
	}
	if len(threadFilter) > 0 && !threadFilter[sub.ThreadID] {
		return false
	}
	if undoStatus != "" && sub.UndoStatus != undoStatus {
		return false
	}
	if before != "" && sub.SendAt >= before {
		return false
	}
	if after != "" && sub.SendAt < after {
		return false
	}
	return true
}

// QuerySubmissions filters, sorts, and pages EmailSubmissions per RFC 8621 Section 7.2.
// Supported filter conditions: identityIds, emailIds, threadIds, undoStatus, before, after.
// Sorting supports the RFC 8621 Section 7.2 properties emailId, threadId, sendAt, undoStatus
// ("sentAt" is accepted as an alias for sendAt); the default sort is sendAt descending.
func (mb *MemoryBackend) QuerySubmissions(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var matched []*jmap.EmailSubmission
	for _, sub := range us.submissions {
		if MatchSubmissionFilter(sub, filter) {
			matched = append(matched, sub)
		}
	}

	SortSubmissions(matched, comparators)

	total := len(matched)
	position = jmap.NormalizePosition(position, total)
	if position > total {
		return []jmap.Id{}, total, nil
	}

	end := total
	if limit != nil {
		l := int(*limit)
		if position+l < end {
			end = position + l
		}
	}

	ids := make([]jmap.Id, 0, end-position)
	for _, sub := range matched[position:end] {
		ids = append(ids, sub.ID)
	}
	return ids, total, nil
}

// SortSubmissions sorts EmailSubmissions in-place per RFC 8621 Section 7.2 comparators.
// Supported properties: emailId, threadId, sendAt, undoStatus (and "sentAt" as an alias per the RFC
// 8621 sort list). The default sort is sendAt descending.
func SortSubmissions(subs []*jmap.EmailSubmission, comparators []jmap.Comparator) {
	if len(comparators) == 0 {
		comparators = []jmap.Comparator{
			{Property: "sendAt", IsAscending: false},
		}
	}

	sort.SliceStable(subs, func(i, j int) bool {
		a, b := subs[i], subs[j]
		for _, comp := range comparators {
			var cmp int
			switch comp.Property {
			case "emailId":
				cmp = strings.Compare(string(a.EmailID), string(b.EmailID))
			case "threadId":
				cmp = strings.Compare(string(a.ThreadID), string(b.ThreadID))
			case "sendAt", "sentAt":
				cmp = strings.Compare(a.SendAt, b.SendAt)
			case "undoStatus":
				cmp = strings.Compare(a.UndoStatus, b.UndoStatus)
			}
			if cmp != 0 {
				if !comp.IsAscending {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return i < j
	})
}

// submissionIDFilter builds a set of Ids from an array-valued filter condition.
func submissionIDFilter(filter map[string]any, key string) map[jmap.Id]bool {
	set := make(map[jmap.Id]bool)
	if raw, ok := filter[key].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				set[jmap.Id(s)] = true
			}
		}
	}
	return set
}

// GetQuotas retrieves requested Quota objects by ID per RFC 9425 Section 4.
func (mb *MemoryBackend) GetQuotas(ctx context.Context, ids []jmap.Id) ([]*jmap.Quota, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.Quota
	var notFound []jmap.Id

	for _, id := range ids {
		if q, ok := us.quotas[id]; ok {
			list = append(list, q)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllQuotas retrieves all Quota objects per RFC 9425 Section 4.
func (mb *MemoryBackend) GetAllQuotas(ctx context.Context) ([]*jmap.Quota, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Quota, 0, len(us.quotas))
	for _, q := range us.quotas {
		list = append(list, q)
	}
	return list, nil
}

// SendMDN sends a Message Disposition Notification per RFC 9007 Section 3.1.
func (mb *MemoryBackend) SendMDN(ctx context.Context, mdn *jmap.MDN) (*jmap.MDN, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	targetEmail, ok := us.emails[mdn.ForEmailID]
	if !ok {
		return nil, fmt.Errorf("email %s not found", mdn.ForEmailID)
	}

	if mdn.ID == "" {
		mb.idCounter++
		mdn.ID = jmap.Id(fmt.Sprintf("mdn-%d", mb.idCounter))
	}

	if mdn.Subject == "" {
		mdn.Subject = fmt.Sprintf("Disposition Notification: %s", targetEmail.Subject)
	}

	if mdn.ReportingUA == "" {
		mdn.ReportingUA = "imap-jmap-server/1.0"
	}

	partID1 := "1"
	mdnEmail := &jmap.Email{
		ID:         mb.nextID("email"),
		BlobID:     jmap.Id(fmt.Sprintf("blob-mdn-%d", mb.idCounter)),
		ThreadID:   targetEmail.ThreadID,
		MailboxIDs: map[jmap.Id]bool{"mb-sent": true},
		Keywords:   map[string]bool{"$seen": true},
		Subject:    mdn.Subject,
		ReceivedAt: time.Now().UTC().Format(time.RFC3339),
		BodyValues: map[string]jmap.EmailBodyValue{
			"1": {Value: fmt.Sprintf("MDN for email %s: %s (%s/%s)", mdn.ForEmailID, mdn.Disposition.Type, mdn.Disposition.ActionMode, mdn.Disposition.SendingMode)},
		},
		TextBody: []jmap.EmailBodyPart{{PartID: &partID1, Type: "text/plain"}},
	}
	us.emails[mdnEmail.ID] = mdnEmail

	// RFC 9007 Section 3.1: sending an MDN creates an email in the Sent mailbox; the
	// Email, Thread, and Mailbox types all change state. MDN itself has no state.
	mb.recalculateMailboxCounts(us)
	mb.recordChange(ctx, us.emailState, mdnEmail.ID, "create", "Email")
	mb.recordChange(ctx, us.threadState, mdnEmail.ThreadID, "update", "Thread")
	mb.recordChange(ctx, us.mailboxState, "mb-sent", "update", "Mailbox")
	return mdn, nil
}

// ParseMDN parses a raw blob containing an MDN message per RFC 9007 Section 3.2.
// The blobID must reference an existing message blob; otherwise the error
// jmap.ErrBlobNotFound is returned so the caller may report it as notFound.
func (mb *MemoryBackend) ParseMDN(ctx context.Context, blobID jmap.Id) (*jmap.MDN, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	for _, em := range us.emails {
		if em.BlobID == blobID {
			return &jmap.MDN{
				ID:          jmap.Id("mdn-parsed-" + string(blobID)),
				ForEmailID:  em.ID,
				Subject:     em.Subject,
				ReportingUA: "imap-jmap-server/1.0",
				Disposition: jmap.MDNDisposition{
					ActionMode:  "automatic-action",
					SendingMode: "MDN-sent-automatically",
					Type:        "displayed",
				},
				TextBody: em.Preview,
			}, nil
		}
	}

	return nil, jmap.ErrBlobNotFound
}

// GetPushSubscriptions retrieves PushSubscription objects by ID per RFC 8620 Section 7.2.1.
func (mb *MemoryBackend) GetPushSubscriptions(ctx context.Context, ids []jmap.Id) ([]*jmap.PushSubscription, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.PushSubscription
	var notFound []jmap.Id

	for _, id := range ids {
		if sub, ok := us.pushSubscriptions[id]; ok {
			list = append(list, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllPushSubscriptions retrieves all PushSubscription objects per RFC 8620 Section 7.2.1.
func (mb *MemoryBackend) GetAllPushSubscriptions(ctx context.Context) ([]*jmap.PushSubscription, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.PushSubscription, 0, len(us.pushSubscriptions))
	for _, sub := range us.pushSubscriptions {
		list = append(list, sub)
	}
	return list, nil
}

// CreatePushSubscription creates a new PushSubscription per RFC 8620 Section 7.2.2.
// Upon creation, the server MUST send a PushVerification to the subscription URL.
func (mb *MemoryBackend) CreatePushSubscription(ctx context.Context, sub *jmap.PushSubscription) (*jmap.PushSubscription, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if sub.ID == "" {
		mb.idCounter++
		sub.ID = jmap.Id(fmt.Sprintf("push-%d", mb.idCounter))
	}

	// Set a verification code per RFC 8620 Section 7.2.2.
	verificationCode := fmt.Sprintf("verify-%s-%d", sub.ID, mb.idCounter)
	sub.VerificationCode = &verificationCode

	us.pushSubscriptions[sub.ID] = sub
	return sub, nil
}

// UpdatePushSubscription updates a PushSubscription per RFC 8620 Section 7.2.2.
// Only "expires", "types", and "verificationCode" are mutable per spec.
func (mb *MemoryBackend) UpdatePushSubscription(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.PushSubscription, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	sub, ok := us.pushSubscriptions[id]
	if !ok {
		return nil, fmt.Errorf("push subscription %s not found", id)
	}

	if v, ok := patch["verificationCode"]; ok {
		if s, ok := v.(string); ok {
			if sub.VerificationCode != nil && *sub.VerificationCode == s {
				sub.VerificationCode = nil
			} else {
				sub.VerificationCode = &s
			}
		} else if v == nil {
			sub.VerificationCode = nil
		}
	}
	if v, ok := patch["expires"]; ok {
		if s, ok := v.(string); ok {
			sub.Expires = &s
		} else if v == nil {
			sub.Expires = nil
		}
	}
	if v, ok := patch["types"]; ok {
		if arr, ok := v.([]any); ok {
			types := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					types = append(types, s)
				}
			}
			sub.Types = types
		} else if v == nil {
			sub.Types = nil
		}
	}

	us.pushSubscriptions[id] = sub
	return sub, nil
}

// DeletePushSubscription deletes a PushSubscription per RFC 8620 Section 7.2.2.
func (mb *MemoryBackend) DeletePushSubscription(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if _, ok := us.pushSubscriptions[id]; !ok {
		return false, nil
	}
	delete(us.pushSubscriptions, id)
	return true, nil
}
