package jmap

// PushSubscriptionKeys holds client-provided encryption keys for push message encryption per RFC 8620 Section 7.2.
type PushSubscriptionKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// PushSubscription represents a JMAP Web Push subscription per RFC 8620 Section 7.2.
type PushSubscription struct {
	ID               Id                    `json:"id,omitempty"`
	DeviceClientID   string                `json:"deviceClientId"`
	URL              string                `json:"url"`
	Keys             *PushSubscriptionKeys `json:"keys,omitempty"`
	VerificationCode *string               `json:"verificationCode,omitempty"`
	Expires          *string               `json:"expires,omitempty"`
	Types            []string              `json:"types,omitempty"`
}
