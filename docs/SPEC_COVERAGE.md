# Specification Conformance & Requirements Traceability

This document is automatically generated from the Go-native specification matrices in [`spec/`](../spec/).

To verify or regenerate this document, run:
```bash
UPDATE_DOCS=1 go test -run TestSpecMarkdownGolden ./spec
```

## Summary

| Matrix | Spec(s) | Covered | Gaps | Non-Goals | Total | Conformance |
| :--- | :--- | :---: | :---: | :---: | :---: | :---: |
| [jmap-calendars](#jmap-calendars) | RFC5545, RFC5546, RFC6047, RFC8620, RFC8984, draft-ietf-jmap-calendars-27 | 45 | 0 | 0 | 45 | 100.0% |
| [jmap-mail](#jmap-mail) | RFC2045, RFC5228, RFC5322, RFC8620, RFC8621, RFC9007, RFC9219, RFC9661 | 39 | 0 | 0 | 39 | 100.0% |
| [jmap-websockets](#jmap-websockets) | RFC8887 | 7 | 0 | 0 | 7 | 100.0% |
| [jscontact](#jscontact) | RFC9553, RFC9554, RFC9555 | 6 | 0 | 0 | 6 | 100.0% |
| [smtp](#smtp) | RFC4954, RFC5228, RFC5232, RFC5321, RFC5429, RFC5546, RFC6047, RFC6376, RFC6409, RFC7208, RFC7489, RFC8601 | 46 | 0 | 0 | 46 | 100.0% |
| **Total** | | **143** | **0** | **0** | **143** | **100.0%** |

---

## jmap-calendars

* **Test Suite Directory**: [`jmap/`](../jmap/)
* **Conformance**: 45 / 45 (100.0%)

| Spec | Section | Level | Requirement | Status | Tests |
| :--- | :---: | :---: | :--- | :---: | :--- |
| RFC5545 | 3.3.11 | `MUST` | TEXT values escape backslash, comma, semicolon and newline on write and unescape on read. | ✅ Covered | `TestRFC5545_TextEscapingRoundTrip` |
| RFC5545 | 3.6.1 | `MUST` | VEVENT properties (recurrence, participants, alarms, location, timezone, duration) round-trip losslessly to and from JSCalendar. | ✅ Covered | `TestRFC5546_ITIPRoundTripFullFidelity` |
| RFC5546 | 2.1.5 | `MUST` | iTIP messages use the event's uid (with SEQUENCE) as the cross-system correlation key, not the server-assigned JMAP id. | ✅ Covered | `TestRFC5546_ITIPUsesEventUIDAndSequence` |
| RFC5546 | 3.2.2 | `MUST` | A REQUEST invitation carries the event's UID, SEQUENCE, ORGANIZER, and ATTENDEE lines. | ✅ Covered | `TestRFC5546_BuildRequestAndCancel`<br/>`TestRFC5546_ITIPRoundTripFullFidelity`<br/>`TestRFC5546_ITIPUsesEventUIDAndSequence`<br/>`TestRFC8984_SchedulingRequestExcludesOwner` |
| RFC5546 | 3.2.3 | `MUST` | A REPLY carries the ORGANIZER being answered and the replying ATTENDEE with its PARTSTAT. | ✅ Covered | `TestRFC5546_BuildAndParseReply`<br/>`TestRFC5546_ITIPUsesEventUIDAndSequence`<br/>`TestRFC8984_SchedulingReplyOnRSVP` |
| RFC5546 | 3.2.5 | `MUST` | A CANCEL carries STATUS:CANCELLED with the event's UID and SEQUENCE. | ✅ Covered | `TestRFC5546_BuildRequestAndCancel`<br/>`TestRFC6047_AutoSendInvitationAndCancellation` |
| RFC6047 | 2.4 | `MUST` | The iMIP body part is text/calendar with a method parameter matching the iCalendar METHOD. | ✅ Covered | `TestRFC6047_AutoSendInvitationAndCancellation`<br/>`TestRFC8984_SchedulingRequestExcludesOwner` |
| RFC8620 | 5.3 | `MUST` | A */set update response value is null unless the server changed properties beyond those the client sent. | ✅ Covered | `TestRFC8984_ParticipantIdentityLifecycle` |
| RFC8620 | 5.4 | `MUST` | Foo/copy reads sources from fromAccountId and supports onSuccessDestroyOriginal / destroyFromIfInState. | ✅ Covered | `TestRFC8984_CalendarEventCopyDestroyOriginal`<br/>`TestRFC8984_CalendarEventCopyRoundTrip` |
| RFC8620 | 5.5 | `MUST` | FilterOperator AND/OR/NOT is evaluated over nested conditions. | ✅ Covered | `TestRFC8984_QueryFilterOperator` |
| RFC8984 | 1.4.5 | `MUST` | LocalDateTime (floating, no time zone) is accepted for date-time values. | ✅ Covered | `TestRFC8984_QueryFloatingLocalDateTimeBounds` |
| RFC8984 | 4.1.2 | `MUST` | uid is required and stable; the server auto-generates one when absent. | ✅ Covered | `TestRFC8984_UIDAutoGenerateAndPersistence` |
| RFC8984 | 4.3.3 | `MUST` | recurrenceRules expand honouring byDay. | ✅ Covered | `TestRFC8984_RecurrenceByDay` |
| RFC8984 | 4.3.3.1 | `MUST` | bySetPosition is applied during recurrence expansion. | ✅ Covered | `TestRFC8984_RecurrenceBySetPosition` |
| RFC8984 | 4.3.4 | `MUST` | excludedRecurrenceRules subtract instances from the expansion. | ✅ Covered | `TestRFC8984_ExcludedRecurrenceRules` |
| RFC8984 | 4.3.5 | `MUST` | recurrenceOverrides apply to instances; excluded:true removes an instance. | ✅ Covered | `TestRFC8984_RecurrenceOverrideExcluded` |
| RFC8984 | 4.4.2 | `MUST` | Event status is limited to confirmed/tentative/cancelled. | ✅ Covered | `TestRFC8984_EventStatusEnum` |
| RFC8984 | 5.2.5 | `MUST` | Task progress is limited to needs-action/in-process/completed/failed/pending/cancelled. | ✅ Covered | `TestRFC8984_TaskProgressEnum` |
| draft-ietf-jmap-calendars-27 | 3.3 | `MUST` | ParticipantIdentity isDefault is server-set; changed only via onSuccessSetIsDefault. | ✅ Covered | `TestRFC8984_ParticipantIdentityLifecycle` |
| draft-ietf-jmap-calendars-27 | 4.2.7 | `MUST` | The scheduleStatus property represents the status of scheduling message delivery as a STATCODE string. | ✅ Covered | `TestRFC8984_SEC7_ScheduleStatusReporting` |
| draft-ietf-jmap-calendars-27 | 4.2.10 | `MUST` | privacy=private: only non-owner sharees get the reduced property set; the owner sees full data. | ✅ Covered | `TestRFC8984_PrivacyOwnerSeesFullData` |
| draft-ietf-jmap-calendars-27 | 4.2.10 | `MUST` | privacy=secret: the server behaves as though the event does not exist for users other than the owner; the owner still sees it. | ✅ Covered | `TestRFC8984_PrivacyOwnerSeesFullData` |
| draft-ietf-jmap-calendars-27 | 4.4 | `MUST` | utcStart/utcEnd computed read-only CalendarEvent properties are returned when requested. | ✅ Covered | `TestRFC8984_UTCStartAndUTCEndComputedProperties` |
| draft-ietf-jmap-calendars-27 | 4.4.5 | `MAY` | hideAttendees limits participant visibility to owners; it round-trips through set/get. | ✅ Covered | `TestRFC8984_HideAttendeesRoundTrip` |
| draft-ietf-jmap-calendars-27 | 5.9 | `MUST` | sendSchedulingMessages (default false): when true the server sends iTIP scheduling messages after a successful create/update/destroy. | ✅ Covered | `TestRFC6047_AutoSendInvitationAndCancellation` |
| draft-ietf-jmap-calendars-27 | 5.9 | `MUST` | noSupportedScheduleMethods is returned when scheduling is requested but no schedule method is available. | ✅ Covered | `TestRFC8984_SchedulingNoSupportedScheduleMethods` |
| draft-ietf-jmap-calendars-27 | 5.9.2.1 | `MUST` | On create/update the origin sends a REQUEST to every current participant except the calendar owner. | ✅ Covered | `TestRFC8984_SchedulingRequestExcludesOwner` |
| draft-ietf-jmap-calendars-27 | 5.9.2.1 | `MUST` | With hideAttendees, each REQUEST contains only its recipient (plus the owner); other attendees are omitted. | ✅ Covered | `TestRFC8984_SchedulingHideAttendees` |
| draft-ietf-jmap-calendars-27 | 5.9.2.1 | `MUST` | A REQUEST to a local participant delivers the event into that participant's own calendar with participation still pending. | ✅ Covered | `TestRFC8984_SameServerInviteAcceptRoundTrip` |
| draft-ietf-jmap-calendars-27 | 5.9.2.2 | `MUST` | On destroy the origin sends a CANCEL to every participant except the calendar owner. | ✅ Covered | `TestRFC6047_AutoSendInvitationAndCancellation` |
| draft-ietf-jmap-calendars-27 | 5.9.2.3 | `MUST` | When not the origin, a REPLY is sent to the organizer when a user participant's participationStatus changes to a value other than needs-action. | ✅ Covered | `TestRFC8984_SchedulingReplyOnRSVP` |
| draft-ietf-jmap-calendars-27 | 5.9.2.3 | `MUST` | A REPLY from a local participant is reflected into the organizer's own copy of the event. | ✅ Covered | `TestRFC8984_SameServerInviteAcceptRoundTrip` |
| draft-ietf-jmap-calendars-27 | 5.11 | `MAY` | expandRecurrences returns per-occurrence ids. | ✅ Covered | `TestRFC8984_ExpandRecurrencesQueryArg`<br/>`TestRFC8984_RecurrenceExpansionQuery` |
| draft-ietf-jmap-calendars-27 | 5.11 | `MUST` | The timeZone argument (default Etc/UTC) is used to interpret before/after bounds. | ✅ Covered | `TestRFC8984_QueryFloatingLocalDateTimeBounds` |
| draft-ietf-jmap-calendars-27 | 5.11 | `SHOULD` | canCalculateChanges is false when expandRecurrences is set (synthetic occurrence ids are not change-tracked). | ✅ Covered | `TestRFC8984_QueryCanCalculateChanges` |
| draft-ietf-jmap-calendars-27 | 5.11 | `SHOULD` | minDateTime/maxDateTime/maxExpandedQueryDuration capability limits are enforced on queries. | ✅ Covered | `TestRFC8984_CalendarQueryCapabilityLimits` |
| draft-ietf-jmap-calendars-27 | 5.11.1 | `MAY` | inCalendars matches events in any listed calendar. | ✅ Covered | `TestRFC8984_InCalendarsFilter` |
| draft-ietf-jmap-calendars-27 | 5.11.1 | `MUST` | before/after are LocalDateTime, matched against the event's start/end in the timeZone argument. | ✅ Covered | `TestRFC8984_QueryFloatingLocalDateTimeBounds` |
| draft-ietf-jmap-calendars-27 | 5.11.1 | `MUST` | before/after accept date-only (LocalDate) values. | ✅ Covered | `TestRFC8984_QueryLocalDateBounds` |
| draft-ietf-jmap-calendars-27 | 5.11.1 | `MUST` | unknown filter conditions are rejected with unsupportedFilter (no fallthrough match). | ✅ Covered | `TestRFC8984_QueryRejectsUnknownFilter` |
| draft-ietf-jmap-calendars-27 | 5.11.1 | `SHOULD` | before/after are DST-correct in a non-UTC timeZone. | ✅ Covered | `TestRFC8984_QueryDSTTransitionBounds` |
| draft-ietf-jmap-calendars-27 | 5.11.2 | `MUST` | sort comparators are limited to supported properties; unknown -> unsupportedSort. | ✅ Covered | `TestRFC8984_QueryRejectsUnknownSort` |
| draft-ietf-jmap-calendars-27 | 5.11.2 | `MUST` | sort supports start, uid, and recurrenceId. | ✅ Covered | `TestRFC8984_CalendarEventQuerySorting` |
| draft-ietf-jmap-calendars-27 | 5.12 | `MAY` | CalendarEvent/parse converts iCalendar blobs to JSCalendar events. | ✅ Covered | `TestRFC8984_CalendarEventParse` |
| draft-ietf-jmap-calendars-27 | 7.2 | `MUST` | CalendarEventNotification objects are server-created and can be fetched, queried, and destroyed. | ✅ Covered | `TestRFC8984_CalendarEventNotificationLifecycle` |

---

## jmap-mail

* **Test Suite Directory**: [`jmap/`](../jmap/)
* **Conformance**: 39 / 39 (100.0%)

| Spec | Section | Level | Requirement | Status | Tests |
| :--- | :---: | :---: | :--- | :---: | :--- |
| RFC2045 | 5 | `MUST` | MIME multipart messages are parsed into a nested body-part structure. | ✅ Covered | `TestEmailParse_MIMETorture`<br/>`TestRFC2045_MIMEPartStructure` |
| RFC5228 | 2 | `MUST` | Sieve script language syntax and commands are parsed and validated. | ✅ Covered | `TestRFC5228_SieveLanguageValidation` |
| RFC5322 | 3.6 | `MUST` | RFC 5322 messages are parsed into JMAP Email header/address/subject/date fields. | ✅ Covered | `TestEmailParse_MIMETorture`<br/>`TestRFC5322_MessageParsing` |
| RFC8620 | 5.4 | `MUST` | Email/copy recreates emails in the target account. | ✅ Covered | `TestRFC8621_Section4_6_EmailCopyRoundTrip`<br/>`TestRFC8621_Section4_EmailCopy` |
| RFC8620 | 5.4 | `MUST` | Mailbox/copy recreates mailboxes in the target account. | ✅ Covered | `TestRFC8621_MailboxCopy`<br/>`TestRFC8621_Section2_MailboxCopy` |
| RFC8620 | 7.2 | `MUST` | PushSubscription create/get/destroy and encrypted Web Push payload delivery using RFC 8291. | ✅ Covered | `TestRFC8291_AppendixA_KnownAnswerVector`<br/>`TestRFC8620_Section7_2_PushSubscriptionRoundTripAndVerification`<br/>`TestRFC8620_WebPush_DispatchStateChange`<br/>`TestRFC8620_WebPush_EndToEndMutation`<br/>`TestRFC8620_WebPush_SubscriptionGone` |
| RFC8620 | 8.2 | `MUST` | Credential authentication must fail closed: when no credential backend is configured, a plain username == password match MUST NOT be accepted. | ✅ Covered | `TestRFC8620_Auth_OIDCRejectsCredentialsWithoutFallback` |
| RFC8621 | 2.1 | `MUST` | Mailbox/get returns Mailbox objects with rights and counts. | ✅ Covered | `TestRFC8621_Section2_1_MailboxGet` |
| RFC8621 | 2.1 | `MAY` | Optional system mailbox roles are supported. | ✅ Covered | `TestRFC8621_Section2_1_MayProvisions_OptionalSystemRoles` |
| RFC8621 | 2.3 | `MUST` | Mailbox/set create/update/destroy, including notUpdated notFound for a missing id. | ✅ Covered | `TestRFC8621_MailboxSetUpdateMissingNotFound`<br/>`TestRFC8621_Section2_3_MailboxSet`<br/>`TestRFC8621_Section2_5_MailboxUpdate` |
| RFC8621 | 2.4 | `MUST` | Mailbox/query filter conditions and sort, positive and negative. | ✅ Covered | `TestMailboxQuery_HasAnyRole_IsSubscribed`<br/>`TestRFC8621_Section2_4_MailboxFilterPropertiesPosNeg`<br/>`TestRFC8621_Section2_4_MailboxQuery` |
| RFC8621 | 3.1 | `MUST` | Thread/get groups related emails; threadIds filter returns all emails of a thread. | ✅ Covered | `TestRFC8621_Section3_1_ThreadGet`<br/>`TestRFC8621_Section3_ThreadGet` |
| RFC8621 | 4.1 | `MUST` | Email/get returns parsed properties and reconstructed body structure. | ✅ Covered | `TestRFC8621_EmailCreateImplicitBodyPartType`<br/>`TestRFC8621_EmailCreateReconstructsBodyStructure`<br/>`TestRFC8621_Section4_1_EmailGet` |
| RFC8621 | 4.3 | `MUST` | Email/set create/update applies partial patches (incl. keywords/*) without dropping unaddressed properties. | ✅ Covered | `TestRFC8621_EmailSetEmptyFieldsNull`<br/>`TestRFC8621_EmailSetErrorPaths`<br/>`TestRFC8621_Section4_3_EmailSet`<br/>`TestRFC8621_Section4_3_EmailSetUpdateKeywords`<br/>`TestRFC8621_Section4_4_EmailSetPartialUpdateDataLossPrevention` |
| RFC8621 | 4.4 | `MUST` | Email/query filter conditions, positive and negative. | ✅ Covered | `TestRFC8621_EmailQueryAllFilterConditions`<br/>`TestRFC8621_EmailQueryFilters_PositiveAndNegative`<br/>`TestRFC8621_Section4_5_1_EmailQueryFromToFilters`<br/>`TestRFC8621_Section4_5_EmailFilterPropertiesPosNeg`<br/>`TestRFC8621_Section4_5_EmailQuery` |
| RFC8621 | 4.4.1 | `MUST` | Email/query text/subject/body/from are free-text searches; a client prefix-wildcard term matches a word. | ✅ Covered | `TestRFC8621_EmailQueryTextSearchWildcard` |
| RFC8621 | 4.4.2 | `MUST` | Email/query sort comparators, including base-subject and multi-comparator tie-breaks. | ✅ Covered | `TestRFC8621_Section4_4_2_EmailSortBaseSubject`<br/>`TestRFC8621_Section4_4_2_EmailSortComparators`<br/>`TestRFC8621_Section4_4_2_EmailSortMultiComparator`<br/>`TestRFC8621_Section4_4_SortOrderAndMultiComparatorTieBreak` |
| RFC8621 | 4.7 | `MUST` | Email/import imports RFC 5322 blobs; error paths are handled. | ✅ Covered | `TestRFC8621_Section4_7_EmailImport`<br/>`TestRFC8621_Section4_8_EmailImportAndParseErrorPaths`<br/>`TestRFC8621_Section4_EmailImportAndParse` |
| RFC8621 | 4.8 | `MUST` | Email/parse parses blobs into Email objects; error paths are handled. | ✅ Covered | `TestEmailParse_AdversarialEdgeCases`<br/>`TestEmailParse_MIMETorture`<br/>`TestRFC8621_Section4_8_EmailParse` |
| RFC8621 | 5 | `MUST` | SearchSnippet/get returns subject/preview snippets with <mark> highlighting. | ✅ Covered | `TestRFC8621_EmailCopy_SearchSnippet_Sieve_CalendarEvent`<br/>`TestRFC8621_Section5_SearchSnippetGet` |
| RFC8621 | 6.1 | `MUST` | The default Identity email is the account's real address (used as From), not the opaque account id. | ✅ Covered | `TestRFC8621_IdentityEmailIsRealAddress` |
| RFC8621 | 6.3 | `MUST` | Identity/get and Identity/set (incl. notUpdated notFound for a missing id). | ✅ Covered | `TestRFC8621_IdentitySetUpdateMissingNotFound`<br/>`TestRFC8621_Section6_1_IdentityGet`<br/>`TestRFC8621_Section6_IdentitySet` |
| RFC8621 | 7.1 | `MUST` | EmailSubmission/get and /query with envelope and delivery status. | ✅ Covered | `TestEmailSubmission_Envelope_RoundTrip`<br/>`TestRFC8621_Section7_1_EmailSubmissionGet`<br/>`TestRFC8621_Section7_2_EmailSubmissionQuery`<br/>`TestRFC8621_SubmissionQueryAnchor`<br/>`TestRFC8621_SubmissionQueryChanges`<br/>`TestRFC8621_SubmissionQueryFilters`<br/>`TestRFC8621_SubmissionQueryPagination`<br/>`TestRFC8621_SubmissionQuery_FilterOperator`<br/>`TestRFC8621_SubmissionQuery_SortUndoStatus` |
| RFC8621 | 7.5 | `MUST` | EmailSubmission/set sends, and applies onSuccessUpdateEmail / onSuccessDestroyEmail. | ✅ Covered | `TestEmailSubmissionSetDestroyTests`<br/>`TestEmailSubmission_BlobUploadImportSubmissionPipeline`<br/>`TestEmailSubmission_CreationReferences`<br/>`TestEmailSubmission_IfInStateMismatch`<br/>`TestEmailSubmission_ImmutablePropertiesNotUpdated`<br/>`TestEmailSubmission_ImportCreationReferences`<br/>`TestEmailSubmission_LocalDelivery`<br/>`TestEmailSubmission_OnSuccessDestroyEmail`<br/>`TestEmailSubmission_OnSuccessUpdateEmail`<br/>`TestEmailSubmission_OutboundSenderRelay`<br/>`TestEmailSubmission_PushStateChange`<br/>`TestEmailSubmission_UndoStatusCancelFinalCannotCancel`<br/>`TestEmailSubmission_UndoStatusCancelPending`<br/>`TestEmailSubmission_ValidationErrors`<br/>`TestRFC8621_Section7_3_EmailSubmissionSet` |
| RFC8621 | 8 | `MUST` | VacationResponse capability, singleton get/set, and singleton create/destroy rejection. | ✅ Covered | `TestRFC8621_VacationResponseCapability`<br/>`TestRFC8621_VacationResponseGetSet`<br/>`TestRFC8621_VacationResponseIfInState` |
| RFC9007 | 2 | `MUST` | The MDN capability is advertised. | ✅ Covered | `TestRFC9007_SessionCapability` |
| RFC9007 | 3 | `MUST` | MDN/send generates a Message Disposition Notification. | ✅ Covered | `TestRFC9007_MDNSend` |
| RFC9007 | 4 | `MUST` | MDN/parse parses an MDN blob. | ✅ Covered | `TestRFC9007_MDNParse`<br/>`TestRFC9007_MDNParse_FullConformance` |
| RFC9219 | 2 | `MUST` | The S/MIME verification capability is advertised. | ✅ Covered | `TestRFC9219_Section2_Capability` |
| RFC9219 | 3 | `MUST` | Email exposes S/MIME status properties. | ✅ Covered | `TestRFC9219_Section3_EmailSMIMEProperties` |
| RFC9219 | 4 | `MUST` | Email/verifySmime verifies S/MIME signatures. | ✅ Covered | `TestRFC9219_Section4_EmailVerifySmime`<br/>`TestRFC9219_VerifySmimePayloadStructure` |
| RFC9219 | 4.1 | `MUST` | Valid S/MIME signatures validate and return smimeStatus signed when certificate path is trusted. | ✅ Covered | `TestRFC9219_Vectors_UnsignedMessageReturnsNull`<br/>`TestRFC9219_Vectors_ValidSignature` |
| RFC9219 | 4.1.1 | `MUST` | A signature that succeeded verification but uses an untrusted/self-signed certificate returns signed/warning. | ✅ Covered | `TestRFC9219_Vectors_ExpiredCertificate`<br/>`TestRFC9219_Vectors_MalformedSignature`<br/>`TestRFC9219_Vectors_TamperedBody`<br/>`TestRFC9219_Vectors_UntrustedCertificateWarning` |
| RFC9219 | 4.2 | `MUST` | Email/query matches emails by smimeStatus filter condition. | ✅ Covered | `TestRFC9219_Vectors_EmailQueryBySmimeStatus` |
| RFC9661 | 2 | `MUST` | The urn:ietf:params:jmap:sieve capability is advertised in the session. | ✅ Covered | `TestRFC9661_Capability` |
| RFC9661 | 4 | `MUST` | SieveScript/get, SieveScript/set, and SieveScript/query manage Sieve scripts. | ✅ Covered | `TestRFC9661_SieveScript_GetSetQuery`<br/>`TestRFC9661_SieveScript_Validate` |
| RFC9661 | 4.2 | `MUST` | SieveScript/set notUpdated notFound is returned when updating a missing script. | ✅ Covered | `TestRFC9661_SieveScriptSetUpdateMissingNotFound` |
| RFC9661 | 4.3 | `MUST` | SieveScript/query filters by name and isValid properties, positive and negative. | ✅ Covered | `TestRFC9661_SieveScriptFilterPropertiesPosNeg` |
| RFC9661 | 4.4 | `MUST` | SieveScript/queryChanges tracks created, updated, and destroyed scripts across state changes. | ✅ Covered | `TestRFC9661_SieveScriptQueryChanges` |

---

## jmap-websockets

* **Test Suite Directory**: [`jmap/`](../jmap/)
* **Conformance**: 7 / 7 (100.0%)

| Spec | Section | Level | Requirement | Status | Tests |
| :--- | :---: | :---: | :--- | :---: | :--- |
| RFC8887 | 3 | `MUST` | The urn:ietf:params:jmap:websocket capability is advertised in the session. | ✅ Covered | `TestRFC8887_SessionCapability` |
| RFC8887 | 4.1 | `MUST` | WebSocket transport supports standard WebSocket ping/pong framing and clean close handshakes. | ✅ Covered | `TestRFC8887_Autobahn_CloseHandshakes`<br/>`TestRFC8887_Autobahn_PingPong` |
| RFC8887 | 4.3.1 | `MUST` | Only text frames are used for JMAP messages over WebSocket; binary frames are ignored. | ✅ Covered | `TestRFC8887_Autobahn_BinaryFrameIgnored` |
| RFC8887 | 4.3.2 | `MUST` | JMAP Request and Response objects are sent over WebSocket, supporting request pipelining. | ✅ Covered | `TestRFC8887_Autobahn_PipelinedRequests`<br/>`TestRFC8887_WebSocketJMAPRequest` |
| RFC8887 | 4.3.4 | `MUST` | Malformed JSON or oversized messages return RequestError with type notJSON or limit. | ✅ Covered | `TestRFC8887_Autobahn_InvalidJSON`<br/>`TestRFC8887_Autobahn_MaxSizeRequestLimit`<br/>`TestRFC8887_WebSocketInvalidCapability` |
| RFC8887 | 4.3.5.2 | `MUST` | A WebSocketPushEnable object enables push notifications for specified data types or all types if omitted. | ✅ Covered | `TestRFC8887_WebSocketPushEnable` |
| RFC8887 | 4.3.5.3 | `MUST` | A WebSocketPushDisable object disables push notifications over the WebSocket connection. | ✅ Covered | `TestRFC8887_WebSocketPushDisable` |

---

## jscontact

* **Test Suite Directory**: [`jmap/vcardconv/`](../jmap/vcardconv/)
* **Conformance**: 6 / 6 (100.0%)

| Spec | Section | Level | Requirement | Status | Tests |
| :--- | :---: | :---: | :--- | :---: | :--- |
| RFC9553 | 1 | `MUST` | JSContact Card specification | ✅ Covered | `TestJSContactSuiteVectors` |
| RFC9554 | 1 | `MUST` | vCard format extensions for JSContact | ✅ Covered | `TestJSContactSuiteVectors` |
| RFC9555 | 1 | `MUST` | JSContact to and from vCard conversion | ✅ Covered | `TestJSContactSuiteVectors` |
| RFC9555 | 2.5.2 | `MUST` | The FN property converts to the Name object's full property | ✅ Covered | `TestJSContactSuiteVectors` |
| RFC9555 | 2.6.1 | `MUST` | If the JSCOMPS parameter is set, then the Address object's isOrdered property value is true | ✅ Covered | `TestJSContactSuiteVectors` |
| RFC9555 | 3.3.1 | `MUST` | The JSCOMPS parameter value is a structured type value | ✅ Covered | `TestJSContactSuiteVectors` |

---

## smtp

* **Test Suite Directory**: [`smtp/`](../smtp/)
* **Conformance**: 46 / 46 (100.0%)

| Spec | Section | Level | Requirement | Status | Tests |
| :--- | :---: | :---: | :--- | :---: | :--- |
| RFC4954 | 3 | `MUST` | The EHLO keyword value associated with this extension is AUTH, and the AUTH EHLO keyword contains as a parameter a space-separated list of the names of available SASL mechanisms. | ✅ Covered | `TestRFC4954_SMTPAuthOverWire` |
| RFC4954 | 3 | `MUST` | The AUTH extension is appropriate for the submission protocol, not for the unauthenticated inbound MX relay path. | ✅ Covered | `TestRFC6409_MXServerDoesNotAdvertiseAUTH` |
| RFC4954 | 4 | `MUST` | Should the client successfully complete the exchange, the SMTP server issues a 235 reply. | ✅ Covered | `TestRFC4954_SMTPAuthOverWire` |
| RFC4954 | 6 | `SHOULD` | 530 5.7.0 Authentication required: this response SHOULD be returned by any command other than AUTH, EHLO, HELO, NOOP, RSET, or QUIT when server policy requires authentication in order to perform the requested action and authentication is not currently in force. | ✅ Covered | `TestRFC6409_SubmissionRequiresAuthentication`<br/>`TestRFC6409_SubmissionRequiresAuthenticationOverWire` |
| RFC4954 | 6 | `SHOULD` | 535 5.7.8 Authentication credentials invalid: the authentication failed due to invalid or insufficient authentication credentials. | ✅ Covered | `TestRFC4954_AuthFailureOverWire`<br/>`TestRFC4954_AuthPlainInvalidCredentialsRejected` |
| RFC4954 | 7 | `SHOULD` | Upon successful authentication, a server SHOULD use the ESMTPA or the ESMTPSA (when appropriate) keyword in the with clause of the Received header field. | ✅ Covered | `TestRFC4954_ReceivedWithClauseESMTPA`<br/>`TestRFC6409_SubmissionDeliversMessageWithESMTPAReceived` |
| RFC4954 | 9 | `MUST` | If an implementation supports SASL mechanisms that are vulnerable to passive eavesdropping attacks (such as PLAIN), then the implementation MUST support at least one configuration where these SASL mechanisms are not advertised or used without the presence of an external security layer such as TLS. | ✅ Covered | `TestRFC4954_NoInsecureAuthConfigurationOverWire`<br/>`TestRFC4954_PlainMechanismOnlyWithSecureLayer` |
| RFC5228 | 4.1 | `MUST` | The fileinto action files the message into the specified mailbox. | ✅ Covered | `TestRFC5228_SieveFileinto` |
| RFC5228 | 4.2 | `MUST` | The redirect action forwards the message to another email address. | ✅ Covered | `TestRFC5228_SieveRedirect` |
| RFC5228 | 4.3 | `MUST` | The discard action silently drops the message without delivery. | ✅ Covered | `TestRFC5228_SieveDiscard` |
| RFC5232 | 3 | `MUST` | The addflag and setflag actions add or replace IMAP flags and keywords on the message. | ✅ Covered | `TestRFC5228_SieveAddFlag` |
| RFC5321 | 3.4 | `MUST` | A receiving SMTP server rejects recipients it cannot deliver to (relaying denied) rather than accepting the message. | ✅ Covered | `TestRFC5321_RejectExternalRecipient` |
| RFC5321 | 3.7 | `MUST` | A receiving SMTP server must not reply 250 to DATA when the message could not be stored for any recipient; it must return a failure reply. | ✅ Covered | `TestRFC5321_DataStorageFailureReturns451`<br/>`TestSMTPReceiver_MIMETorture`<br/>`TestSMTPReceiver_OversizedMessageDATA` |
| RFC5321 | 4.4 | `MUST` | A receiving SMTP server prepends a Received: trace header to the accepted message content. | ✅ Covered | `TestLocalDelivery_UserToUserOverSMTP` |
| RFC5429 | 2 | `MUST` | The reject action terminates delivery and rejects the message with an SMTP error response. | ✅ Covered | `TestRFC5228_SieveReject` |
| RFC5546 | 3.2.2 | `MUST` | An inbound REQUEST imports the full event (recurrence, duration, location, participants) into the invitee's calendar. | ✅ Covered | `TestRFC6047_InboundRequestFullFidelityMultipart` |
| RFC5546 | 3.2.3 | `MUST` | An inbound REPLY updates the replying attendee's participationStatus on the UID-correlated event, not the event-level status. | ✅ Covered | `TestRFC6047_InboundReplyUpdatesParticipationStatus` |
| RFC6047 | 2.2.2 | `MUST` | Authentication gate: without a configured SenderVerifier (development mode) the gate is skipped. | ✅ Covered | `TestCheckSenderAuth_NoVerifierDevelopmentMode` |
| RFC6047 | 2.2.2 | `MUST` | Local trust exception: loopback clients are trusted without SPF/DKIM/DMARC validation so local delivery works. | ✅ Covered | `TestCheckSenderAuth_LoopbackClientTrusted`<br/>`TestRFC6047_SenderAuth_LocalDeliveryWorksWithoutValidation` |
| RFC6047 | 2.2.2 | `MUST` | Local trust exception: envelope senders that are local accounts of this server are trusted without DNS validation. | ✅ Covered | `TestCheckSenderAuth_LocalAccountSenderTrusted` |
| RFC6047 | 2.2.2 | `MUST` | Unauthenticated messages may not be trusted: verification errors fail closed. | ✅ Covered | `TestCheckSenderAuth_VerifierErrorFailsClosed` |
| RFC6047 | 2.2.2 | `MUST` | Authenticated sender passes the gate and the iTIP REPLY is applied. | ✅ Covered | `TestRFC6047_SenderAuth_DMARCOrgDomainDiscoveryApplies`<br/>`TestRFC6047_SenderAuth_SPFPassAppliesREPLY` |
| RFC6047 | 2.2.2 | `MUST` | Unauthenticated messages may not be trusted: the message is delivered to the mailbox but never mutates calendar state. | ✅ Covered | `TestRFC6047_SenderAuth_DMARCPolicyRejectBlocks`<br/>`TestRFC6047_SenderAuth_SPFFailNoMutation` |
| RFC6047 | 2.2.2 | `MUST` | Authenticated sender passes the gate and the iTIP REQUEST is imported. | ✅ Covered | `TestRFC6047_SenderAuth_DKIMPassImportsREQUEST` |
| RFC6047 | 2.2.2 | `MUST` | Unauthenticated messages may not be trusted: absence of SPF, DKIM and DMARC information fails closed. | ✅ Covered | `TestRFC6047_SenderAuth_NoAuthInfoFailClosed`<br/>`TestVerify_FailClosedWithEmptyDNS` |
| RFC6047 | 2.2.2 | `MUST` | Unauthenticated messages may not be trusted: DNS errors fail closed. | ✅ Covered | `TestRFC6047_SenderAuth_DNSTimeoutFailClosed` |
| RFC6047 | 2.2.2 | `MUST` | Transport-boundary trust: an iTIP message submitted by an RFC 4954 authenticated client on the submission transport is applied without DNS sender authentication. | ✅ Covered | `TestSenderAuth_AuthenticatedSubmissionTrusted` |
| RFC6047 | 2.2.2 | `MUST` | Unauthenticated messages may not be trusted: the same message on the MX transport is blocked because the sender cannot be verified. | ✅ Covered | `TestSenderAuth_MXBoundaryDoesNotTrustEnvelope` |
| RFC6047 | 2.4 | `MUST` | A received text/calendar body with method=REPLY is processed as an iTIP scheduling response. | ✅ Covered | `TestRFC6047_InboundReplyUpdatesParticipationStatus` |
| RFC6047 | 2.4 | `MUST` | The text/calendar part is located and decoded via a MIME parser (honouring Content-Transfer-Encoding), not raw byte scanning. | ✅ Covered | `TestRFC6047_InboundRequestFullFidelityMultipart` |
| RFC6047 | 3 | `MUST` | Security Considerations: the originator of an iCalendar object must be authenticated by a recipient. | ✅ Covered | `TestCheckSenderAuth_ExternalSenderVerified` |
| RFC6376 | 6.1 | `MUST` | Every DKIM-Signature header field of the message is verified; a verified signature authenticates the message. | ✅ Covered | `TestRFC6047_SenderAuth_DKIMPassImportsREQUEST` |
| RFC6409 | 3.1 | `MUST` | Port 587 is reserved for email message submission as specified in this document. Messages received on this port are defined to be submissions. The protocol used is ESMTP, with additional restrictions or allowances as specified here. | ✅ Covered | `TestRFC6409_SubmissionRequiresAuthenticationOverWire` |
| RFC6409 | 4.3 | `MUST` | The MSA MUST, by default, issue an error response to the MAIL command if the session has not been authenticated using SMTP-AUTH, unless it has already independently established authentication or authorization (such as being within a protected subnetwork). | ✅ Covered | `TestRFC6409_SubmissionRequiresAuthentication`<br/>`TestRFC6409_SubmissionRequiresAuthenticationOverWire` |
| RFC6409 | 6.1 | `MAY` | The MSA MAY issue an error response to a MAIL command if the address in MAIL FROM appears to have insufficient submission rights or is not authorized with the authentication used (if the session has been authenticated). Reply code 550 with an appropriate enhanced status code, such as 5.7.1, is used for this purpose. | ✅ Covered | `TestRFC6409_SubmissionSenderMustMatchAuthenticatedIdentity`<br/>`TestRFC6409_SubmissionSenderMustMatchAuthenticatedUserOverWire` |
| RFC6409 | 6.2 | `MAY` | The MSA MAY issue an error response to a RCPT command if inconsistent with the permissions given to the user (if the session has been authenticated). Reply code 550 with an appropriate enhanced status code, such as 5.7.1, is used for this purpose. | ✅ Covered | `TestRFC6409_SubmissionRcptPermissions` |
| RFC6409 | 8.3 | `SHOULD` | The MSA SHOULD add or replace the Message-ID field, if it lacks it, or it is not valid syntax. | ✅ Covered | `TestRFC6409_SubmissionAddsMessageID`<br/>`TestRFC6409_SubmissionDeliversMessageWithESMTPAReceived` |
| RFC7208 | 2.6.3 | `MUST` | A 'pass' result is an explicit statement that the client is authorized to inject mail with the given identity. | ✅ Covered | `TestRFC6047_SenderAuth_SPFPassAppliesREPLY` |
| RFC7208 | 2.6.4 | `MUST` | A 'fail' result is an explicit statement that the client is not authorized to use the domain in the given identity. | ✅ Covered | `TestRFC6047_SenderAuth_SPFFailNoMutation` |
| RFC7208 | 4.6.4 | `MUST` | Check Host() when a DNS error occurs: results other than 'domain does not exist' yield 'temperror'. | ✅ Covered | `TestRFC6047_SenderAuth_DNSTimeoutFailClosed` |
| RFC7489 | 3.1 | `MUST` | Identifier Alignment: strict mode requires an exact FQDN match; relaxed mode compares Organizational Domains. | ✅ Covered | `TestAligned_StrictAndRelaxedModes`<br/>`TestRFC6047_SenderAuth_DMARCOrgDomainDiscoveryApplies` |
| RFC7489 | 3.2 | `MUST` | Organizational Domain heuristic: the registered domain is the last two DNS labels. | ✅ Covered | `TestOrganizationalDomain_Heuristic` |
| RFC7489 | 6.6.1 | `MUST` | Extract Author Domain: messages without a usable RFC5322.From domain are not processed. | ✅ Covered | `TestExtractFromDomain_FailClosedCases` |
| RFC7489 | 6.6.2 | `MUST` | Step 5: if no Authenticated Identifier aligns with the RFC5322.From domain, the message fails the DMARC check. | ✅ Covered | `TestRFC6047_SenderAuth_DMARCPolicyRejectBlocks` |
| RFC7489 | 6.6.3 | `MUST` | Policy Discovery: when no record exists at the RFC5322.From domain, the DMARC record at the Organizational Domain is used. | ✅ Covered | `TestRFC6047_SenderAuth_DMARCOrgDomainDiscoveryApplies` |
| RFC8601 | 3 | `MUST` | Authentication-Results header indicates message authentication status. | ✅ Covered | `TestRFC8601_AuthenticationResultsHeader` |

---

