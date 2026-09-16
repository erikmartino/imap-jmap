package spec

// JMAPSharingRequirements defines the normative requirement matrix for RFC 9670 (JMAP Sharing).
var JMAPSharingRequirements = []Requirement{
	{
		Spec:    "RFC9670",
		Section: "1.4.1",
		Level:   MUST,
		Text:    "The urn:ietf:params:jmap:sharing capability URI MUST be advertised in the accountCapabilities for accounts that support sharing.",
		Tests:   []string{"TestRFC9670_CapabilityAdvertisement"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9670",
		Section: "2",
		Level:   MUST,
		Text:    "A ShareNotification object represents a change to the sharing status of an object.",
		Tests:   []string{"TestRFC9670_ShareNotificationGet", "TestStalwart_CalendarACL"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9670",
		Section: "3",
		Level:   MUST,
		Text:    "ShareNotification/get returns requested properties for share notifications.",
		Tests:   []string{"TestRFC9670_ShareNotificationGet", "TestStalwart_CalendarACL"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9670",
		Section: "4",
		Level:   MUST,
		Text:    "ShareNotification/changes returns changes to share notifications since a specified state.",
		Tests:   []string{"TestRFC9670_ShareNotificationChanges", "TestStalwart_CalendarACL"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9670",
		Section: "4.2",
		Level:   MUST,
		Text:    "ShareNotification/set only supports destroying share notifications; creating or updating is rejected.",
		Tests:   []string{"TestRFC9670_ShareNotificationSetDestroyOnly"},
		Status:  Covered,
	},
	{
		Spec:    "RFC9670",
		Section: "5",
		Level:   MUST,
		Text:    "Cross-account access without appropriate sharing rights MUST be rejected with a forbidden error.",
		Tests:   []string{"TestRFC9670_CrossAccountForbidden"},
		Status:  Covered,
	},
}
