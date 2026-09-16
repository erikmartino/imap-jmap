package jmap

// ShareNotificationPerson represents the changedBy entity in a ShareNotification per RFC 9670 Section 2.
type ShareNotificationPerson struct {
	PrincipalID string `json:"principalId"`
	Name        string `json:"name"`
	Email       string `json:"email"`
}

// ShareNotification represents a notification of a sharing change per RFC 9670 Section 2.
type ShareNotification struct {
	ID              Id                      `json:"id"`
	Type            string                  `json:"@type,omitempty"` // "ShareNotification"
	ChangedBy       ShareNotificationPerson `json:"changedBy"`
	ObjectType      string                  `json:"objectType"`
	ObjectAccountID string                  `json:"objectAccountId"`
	ObjectID        Id                      `json:"objectId"`
	OldRights       *CalendarRights         `json:"oldRights"`
	NewRights       *CalendarRights         `json:"newRights"`
	Name            *string                 `json:"name"`
}
