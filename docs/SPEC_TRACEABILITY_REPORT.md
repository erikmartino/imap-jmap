# Specification Conformance & Requirements Traceability Report

This report maps official IETF RFC normative clauses across all registered domain matrices to their corresponding **Unit Tests** and **Production Implementation Code**.

---

## 1. Executive Conformance Summary

| Specification Matrix | Primary RFC Standards | Covered Clauses | Outstanding Gaps | Non-Goals | Total Requirements | Conformance % |
| :--- | :--- | :---: | :---: | :---: | :---: | :---: |
| **jmap-calendars** | RFC 5545, RFC 5546, RFC 6047, RFC 8620, RFC 8984, draft-ietf-jmap-calendars-27 | 45 | 0 | 0 | 45 | 100.0% |
| **jmap-mail** | RFC 2045, RFC 5228, RFC 5322, RFC 8620, RFC 8621, RFC 9007, RFC 9219, RFC 9661 | 39 | 0 | 0 | 39 | 100.0% |
| **jmap-websockets** | RFC 8887 | 7 | 0 | 0 | 7 | 100.0% |
| **jscontact** | RFC 9553, RFC 9554, RFC 9555 | 6 | 0 | 0 | 6 | 100.0% |
| **smtp** | RFC 4954, RFC 5228, RFC 5232, RFC 5321, RFC 5429, RFC 5546, RFC 6047, RFC 6376, RFC 6409, RFC 7208, RFC 7489, RFC 8601 | 46 | 0 | 0 | 46 | 100.0% |
| **Total** | | **143** | **0** | **0** | **143** | **100.0%** |

---

## 2. Requirement Traceability Matrix

### 2.1 JMAP for Calendars (`jmap-calendars`)

| Spec | Section | Level | Requirement Description | Test File & Function | Production Implementation |
| :--- | :---: | :---: | :--- | :--- | :--- |
| **RFC 5545** | [3.3.11](https://www.rfc-editor.org/rfc/rfc5545.html#section-3.3.11) | `MUST` | TEXT values escape backslash, comma, semicolon and newline on write and unescape on read. | [`TestRFC5545_TextEscapingRoundTrip`](../jmap/rfc5546_roundtrip_test.go) | [`jmap/ical_text_escaping.go`](../jmap/ical_text_escaping.go) |
| **RFC 5545** | [3.6.1](https://www.rfc-editor.org/rfc/rfc5545.html#section-3.6.1) | `MUST` | VEVENT properties (recurrence, participants, alarms, location, timezone, duration) round-trip losslessly to and from JSCalendar. | [`TestRFC5546_ITIPRoundTripFullFidelity`](../jmap/rfc8984_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **RFC 5546** | [2.1.5](https://www.rfc-editor.org/rfc/rfc5546.html#section-2.1.5) | `MUST` | iTIP messages use the event's uid (with SEQUENCE) as the cross-system correlation key, not the server-assigned JMAP id. | [`TestRFC5546_ITIPUsesEventUIDAndSequence`](../jmap/rfc8984_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **RFC 5546** | [3.2.2](https://www.rfc-editor.org/rfc/rfc5546.html#section-3.2.2) | `MUST` | A REQUEST invitation carries the event's UID, SEQUENCE, ORGANIZER, and ATTENDEE lines. | [`TestRFC5546_BuildRequestAndCancel`](../jmap/rfc8984_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **RFC 5546** | [3.2.3](https://www.rfc-editor.org/rfc/rfc5546.html#section-3.2.3) | `MUST` | A REPLY carries the ORGANIZER being answered and the replying ATTENDEE with its PARTSTAT. | [`TestRFC5546_BuildAndParseReply`](../jmap/rfc8984_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **RFC 5546** | [3.2.5](https://www.rfc-editor.org/rfc/rfc5546.html#section-3.2.5) | `MUST` | A CANCEL carries STATUS:CANCELLED with the event's UID and SEQUENCE. | [`TestRFC5546_BuildRequestAndCancel`](../jmap/rfc8984_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **RFC 6047** | [2.4](https://www.rfc-editor.org/rfc/rfc6047.html#section-2.4) | `MUST` | The iMIP body part is text/calendar with a method parameter matching the iCalendar METHOD. | [`TestRFC6047_AutoSendInvitationAndCancellation`](../jmap/rfc8984_test.go) | [`smtp/rfc6047.go`](../smtp/rfc6047.go) |
| **RFC 8620** | [5.3](https://www.rfc-editor.org/rfc/rfc8620.html#section-5.3) | `MUST` | A */set update response value is null unless the server changed properties beyond those the client sent. | [`TestRFC8984_ParticipantIdentityLifecycle`](../jmap/rfc8984_test.go) | [`jmap/calendarevent_set.go`](../jmap/calendarevent_set.go) |
| **RFC 8620** | [5.4](https://www.rfc-editor.org/rfc/rfc8620.html#section-5.4) | `MUST` | Foo/copy reads sources from fromAccountId and supports onSuccessDestroyOriginal / destroyFromIfInState. | [`TestRFC8984_CalendarEventCopyRoundTrip`](../jmap/rfc8984_test.go) | [`jmap/calendarevent_copy.go`](../jmap/calendarevent_copy.go) |
| **RFC 8620** | [5.5](https://www.rfc-editor.org/rfc/rfc8620.html#section-5.5) | `MUST` | FilterOperator AND/OR/NOT is evaluated over nested conditions. | [`TestRFC8984_QueryFilterOperator`](../jmap/rfc8984_test.go) | [`jmap/calendarevent_query.go`](../jmap/calendarevent_query.go) |
| **RFC 8984** | [1.4.5](https://www.rfc-editor.org/rfc/rfc8984.html#section-1.4.5) | `MUST` | LocalDateTime (floating, no time zone) is accepted for date-time values. | [`TestRFC8984_QueryFloatingLocalDateTimeBounds`](../jmap/rfc8984_query_dst_test.go) | [`jmap/calendarevent_query.go`](../jmap/calendarevent_query.go) |
| **RFC 8984** | [4.1.2](https://www.rfc-editor.org/rfc/rfc8984.html#section-4.1.2) | `MUST` | uid is required and stable; the server auto-generates one when absent. | [`TestRFC8984_UIDAutoGenerateAndPersistence`](../jmap/rfc8984_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **RFC 8984** | [4.3.3](https://www.rfc-editor.org/rfc/rfc8984.html#section-4.3.3) | `MUST` | recurrenceRules expand honouring byDay. | [`TestRFC8984_RecurrenceByDay`](../jmap/rfc8984_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **RFC 8984** | [4.3.4](https://www.rfc-editor.org/rfc/rfc8984.html#section-4.3.4) | `MUST` | excludedRecurrenceRules subtract instances from the expansion. | [`TestRFC8984_ExcludedRecurrenceRules`](../jmap/rfc8984_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **draft-ietf-jmap-calendars-27** | [3.3](https://datatracker.ietf.org/doc/html/draft-ietf-jmap-calendars-27#section-3.3) | `MUST` | ParticipantIdentity isDefault is server-set; changed only via onSuccessSetIsDefault. | [`TestRFC8984_ParticipantIdentityLifecycle`](../jmap/rfc8984_test.go) | [`jmap/participantidentity_set.go`](../jmap/participantidentity_set.go) |
| **draft-ietf-jmap-calendars-27** | [4.2.10](https://datatracker.ietf.org/doc/html/draft-ietf-jmap-calendars-27#section-4.2.10) | `MUST` | privacy=private: only non-owner sharees get the reduced property set; the owner sees full data. | [`TestRFC8984_PrivacyOwnerSeesFullData`](../jmap/rfc8984_privacy_and_freebusy_rights_test.go) | [`jmap/nextcloud/calendars.go`](../jmap/nextcloud/calendars.go) |
| **draft-ietf-jmap-calendars-27** | [5.9](https://datatracker.ietf.org/doc/html/draft-ietf-jmap-calendars-27#section-5.9) | `MUST` | sendSchedulingMessages (default false): when true the server sends iTIP scheduling messages after a successful create/update/destroy. | [`TestRFC6047_AutoSendInvitationAndCancellation`](../jmap/rfc8984_test.go) | [`jmap/calendarevent_set.go`](../jmap/calendarevent_set.go) |
| **draft-ietf-jmap-calendars-27** | [5.11](https://datatracker.ietf.org/doc/html/draft-ietf-jmap-calendars-27#section-5.11) | `MAY` | expandRecurrences returns per-occurrence ids. | [`TestRFC8984_ExpandRecurrencesQueryArg`](../jmap/rfc8984_test.go) | [`jmap/calendarevent_query.go`](../jmap/calendarevent_query.go) |

---

### 2.2 JMAP for Mail (`jmap-mail`)

| Spec | Section | Level | Requirement Description | Test File & Function | Production Implementation |
| :--- | :---: | :---: | :--- | :--- | :--- |
| **RFC 2045** | [5](https://www.rfc-editor.org/rfc/rfc2045.html#section-5) | `MUST` | MIME multipart messages are parsed into a nested body-part structure. | [`TestEmailParse_MIMETorture`](../jmap/email_parse_test.go) | [`smtp/parser.go`](../smtp/parser.go) |
| **RFC 5228** | [2](https://www.rfc-editor.org/rfc/rfc5228.html#section-2) | `MUST` | Sieve script language syntax and commands are parsed and validated. | [`TestRFC5228_SieveLanguageValidation`](../jmap/rfc5228_test.go) | [`jmap/managesieve/backend.go`](../jmap/managesieve/backend.go) |
| **RFC 5322** | [3.6](https://www.rfc-editor.org/rfc/rfc5322.html#section-3.6) | `MUST` | RFC 5322 messages are parsed into JMAP Email header/address/subject/date fields. | [`TestRFC5322_MessageParsing`](../jmap/email_parse_test.go) | [`smtp/parser.go`](../smtp/parser.go) |
| **RFC 8620** | [5.4](https://www.rfc-editor.org/rfc/rfc8620.html#section-5.4) | `MUST` | Email/copy recreates emails in the target account. | [`TestRFC8621_Section4_EmailCopy`](../jmap/rfc8621_email_copy_test.go) | [`jmap/email_copy.go`](../jmap/email_copy.go) |
| **RFC 8620** | [7.2](https://www.rfc-editor.org/rfc/rfc8620.html#section-7.2) | `MUST` | PushSubscription create/get/destroy and encrypted Web Push payload delivery using RFC 8291. | [`TestRFC8620_Section7_2_PushSubscriptionRoundTripAndVerification`](../jmap/rfc8620_push_test.go) | [`jmap/push.go`](../jmap/push.go) |
| **RFC 8620** | [8.2](https://www.rfc-editor.org/rfc/rfc8620.html#section-8.2) | `MUST` | Credential authentication must fail closed when no backend is configured. | [`TestRFC8620_Auth_OIDCRejectsCredentialsWithoutFallback`](../jmap/oidc_auth_test.go) | [`jmap/oidc_auth.go`](../jmap/oidc_auth.go) |
| **RFC 8621** | [2.1](https://www.rfc-editor.org/rfc/rfc8621.html#section-2.1) | `MUST` | Mailbox/get returns Mailbox objects with rights and counts. | [`TestRFC8621_Section2_1_MailboxGet`](../jmap/rfc8621_mailbox_test.go) | [`jmap/mailbox_get.go`](../jmap/mailbox_get.go) |
| **RFC 8621** | [2.3](https://www.rfc-editor.org/rfc/rfc8621.html#section-2.3) | `MUST` | Mailbox/set create/update/destroy, including notUpdated notFound for missing id. | [`TestRFC8621_MailboxSetUpdateMissingNotFound`](../jmap/rfc8621_mailbox_test.go) | [`jmap/mailbox_set.go`](../jmap/mailbox_set.go) |
| **RFC 8621** | [3.1](https://www.rfc-editor.org/rfc/rfc8621.html#section-3.1) | `MUST` | Thread/get groups related emails; threadIds filter returns all emails of a thread. | [`TestRFC8621_Section3_1_ThreadGet`](../jmap/rfc8621_thread_test.go) | [`jmap/thread_get.go`](../jmap/thread_get.go) |
| **RFC 8621** | [4.1](https://www.rfc-editor.org/rfc/rfc8621.html#section-4.1) | `MUST` | Email/get returns parsed properties and reconstructed body structure. | [`TestRFC8621_Section4_1_EmailGet`](../jmap/rfc8621_email_get_test.go) | [`jmap/email_get.go`](../jmap/email_get.go) |
| **RFC 8621** | [4.3](https://www.rfc-editor.org/rfc/rfc8621.html#section-4.3) | `MUST` | Email/set create/update applies partial patches without dropping unaddressed properties. | [`TestRFC8621_Section4_4_EmailSetPartialUpdateDataLossPrevention`](../jmap/rfc8621_email_set_test.go) | [`jmap/email_set.go`](../jmap/email_set.go) |
| **RFC 8621** | [4.4](https://www.rfc-editor.org/rfc/rfc8621.html#section-4.4) | `MUST` | Email/query filter conditions, positive and negative. | [`TestRFC8621_EmailQueryAllFilterConditions`](../jmap/rfc8621_email_query_test.go) | [`jmap/email_query.go`](../jmap/email_query.go) |

---

### 2.3 SMTP & Delivery Protocols (`smtp`)

| Spec | Section | Level | Requirement Description | Test File & Function | Production Implementation |
| :--- | :---: | :---: | :--- | :--- | :--- |
| **RFC 4954** | [4.3](https://www.rfc-editor.org/rfc/rfc4954.html#section-4.3) | `MUST` | Plain SASL mechanism must only be offered/accepted over TLS. | [`TestRFC4954_PlainMechanismOnlyWithSecureLayer`](../smtp/rfc6409_submission_test.go) | [`smtp/server.go`](../smtp/server.go) |
| **RFC 5321** | [3.4](https://www.rfc-editor.org/rfc/rfc5321.html#section-3.4) | `MUST` | Receiving SMTP server rejects external recipients (relaying denied). | [`TestRFC5321_RejectExternalRecipient`](../smtp/rfc5321_reject_external_test.go) | [`smtp/server.go`](../smtp/server.go) |
| **RFC 5321** | [3.7](https://www.rfc-editor.org/rfc/rfc5321.html#section-3.7) | `MUST` | Receiving SMTP server returns failure reply on DATA storage error. | [`TestRFC5321_DataStorageFailureReturns451`](../smtp/mime_torture_test.go) | [`smtp/server.go`](../smtp/server.go) |
| **RFC 5321** | [4.4](https://www.rfc-editor.org/rfc/rfc5321.html#section-4.4) | `MUST` | Receiving SMTP server prepends Received: trace header to accepted message. | [`TestLocalDelivery_UserToUserOverSMTP`](../smtp/local_delivery_test.go) | [`smtp/server.go`](../smtp/server.go) |
| **RFC 6047** | [2.2.2](https://www.rfc-editor.org/rfc/rfc6047.html#section-2.2.2) | `MUST` | Sender authentication gate: DNS errors and unauthenticated senders fail closed. | [`TestRFC6047_SenderAuth_DNSTimeoutFailClosed`](../smtp/sender_auth_itip_test.go) | [`smtp/sender_auth.go`](../smtp/sender_auth.go) |
| **RFC 6409** | [4.3](https://www.rfc-editor.org/rfc/rfc6409.html#section-4.3) | `MUST` | MSA issues error response to MAIL command if session is unauthenticated. | [`TestRFC6409_SubmissionRequiresAuthentication`](../smtp/rfc6409_submission_test.go) | [`smtp/server.go`](../smtp/server.go) |
| **RFC 7208** | [2.6.3](https://www.rfc-editor.org/rfc/rfc7208.html#section-2.6.3) | `MUST` | SPF 'pass' result authorizes client to inject mail with given identity. | [`TestRFC6047_SenderAuth_SPFPassAppliesREPLY`](../smtp/sender_auth_itip_test.go) | [`smtp/sender_auth.go`](../smtp/sender_auth.go) |
| **RFC 7489** | [3.1](https://www.rfc-editor.org/rfc/rfc7489.html#section-3.1) | `MUST` | DMARC Identifier Alignment: strict mode requires exact FQDN match; relaxed mode checks Org Domain. | [`TestAligned_StrictAndRelaxedModes`](../smtp/sender_auth_itip_test.go) | [`smtp/sender_auth.go`](../smtp/sender_auth.go) |

---

## 3. Machine Verification Tools

1. **Matrix Conformance Checker (`spec/spec_test.go`)**:
   ```bash
   go test -v ./spec/ -run TestSpecCoverage
   ```
2. **Bi-Directional AST Traceability (`jmap/spec_coverage_test.go`)**:
   ```bash
   go test -v ./jmap/ -run TestSpecCoverageBidirectional
   ```
3. **Production Code `@spec` Annotation Linter (`jmap/spec_linter_test.go`)**:
   ```bash
   go test -v ./jmap/ -run TestSpecTraceabilityLinter
   ```
