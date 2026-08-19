# JMAP-TestSuite Conformance Status

This document tracks the status of running the official Fastmail `JMAP-TestSuite` (`~/git/fastmail/JMAP-TestSuite`) against `imap-jmap`.

## Summary
- **Total Test Files**: 89
- **Passing**: 18
- **Failing**: 71

---

## Detailed Status by Subsystem

### Core & Transport
| Test File | Status | Notes |
|---|---|---|
| `t/basic.t` | PASS | Basic account, mailbox creation, upload, import |
| `t/core/backrefs-simple.t` | PASS | Result reference resolution |
| `t/core/download.t` | FAIL | |
| `t/core/jmap-session-resource.t` | FAIL | |
| `t/core/upload.t` | FAIL | |

### Mailbox Subsystem
| Test File | Status | Notes |
|---|---|---|
| `t/Mailbox/changes/changed-properties.t` | FAIL | |
| `t/Mailbox/changes/max-changes-has-more-changes.t` | FAIL | |
| `t/Mailbox/changes/no-changes.t` | PASS | |
| `t/Mailbox/changes/with-changes.t` | PASS | |
| `t/Mailbox/get/limiting-properties-in-response.t` | FAIL | |
| `t/Mailbox/get/no-existing-entities.t` | FAIL | |
| `t/Mailbox/get/some-entities.t` | FAIL | |
| `t/Mailbox/query/filtering-with-filter-conditions.t` | FAIL | |
| `t/Mailbox/query/filtering-with-filter-operators.t` | FAIL | |
| `t/Mailbox/query/no-existing-entities.t` | FAIL | |
| `t/Mailbox/query/sorting-and-limiting.t` | FAIL | |
| `t/Mailbox/set/create/all-settable-fields-provided.t` | PASS | |
| `t/Mailbox/set/create/defaults-omitted.t` | FAIL | |
| `t/Mailbox/set/create/immutable-fields.t` | FAIL | |
| `t/Mailbox/set/destroy/good-destroy-no-messages.t` | PASS | |
| `t/Mailbox/set/destroy/mailbox-has-child-error.t` | FAIL | |
| `t/Mailbox/set/destroy/mailbox-has-email-error.t` | FAIL | |
| `t/Mailbox/set/destroy/on-destroy-remove-messages-true-mail-in-other-boxes.t` | FAIL | |
| `t/Mailbox/set/destroy/on-destroy-remove-messages-true-mail-only-here.t` | FAIL | |
| `t/Mailbox/set/update/basic.t` | FAIL | |

### Thread Subsystem
| Test File | Status | Notes |
|---|---|---|
| `t/Thread/changes/changes.t` | FAIL | |
| `t/Thread/changes/max-changes-has-more-changes.t` | FAIL | |
| `t/Thread/changes/no-changes.t` | FAIL | |
| `t/Thread/get/empty-list.t` | PASS | |
| `t/Thread/get/few-messages.t` | FAIL | |
| `t/Thread/get/unknown-ids.t` | PASS | |

### Email Subsystem
| Test File | Status | Notes |
|---|---|---|
| `t/Email/changes/changes.t` | PASS | |
| `t/Email/changes/max-changes-has-more-changes.t` | FAIL | |
| `t/Email/changes/no-changes.t` | PASS | |
| `t/Email/get/attachments.t` | FAIL | |
| `t/Email/get/body-properties.t` | FAIL | |
| `t/Email/get/body-structure-and-attachments.t` | FAIL | |
| `t/Email/get/fetch-all-body-values.t` | FAIL | |
| `t/Email/get/fetch-html-body-values.t` | FAIL | |
| `t/Email/get/fetch-text-body-values.t` | FAIL | |
| `t/Email/get/has-attachment.t` | FAIL | |
| `t/Email/get/header-header-field-name.t` | FAIL | |
| `t/Email/get/html-body.t` | FAIL | |
| `t/Email/get/max-body-value-bytes.t` | FAIL | |
| `t/Email/get/no-ids.t` | PASS | |
| `t/Email/get/properties.t` | FAIL | |
| `t/Email/get/text-body.t` | FAIL | |
| `t/Email/import/bad-values.t` | FAIL | |
| `t/Email/import/good-imports.t` | FAIL | |
| `t/Email/import/invalid-email.t` | PASS | |
| `t/Email/import/one-fails-another-succeeds.t` | FAIL | |
| `t/Email/queryChanges/simple.t` | FAIL | |
| `t/Email/query/filtering.t` | FAIL | |
| `t/Email/query/no-existing-entities.t` | FAIL | |
| `t/Email/set/create/attachments.t` | FAIL | |
| `t/Email/set/create/attachments-using-blob-id.t` | FAIL | |
| `t/Email/set/create/blob-not-found.t` | FAIL | |
| `t/Email/set/create/body-structure-cannot-duplicate-headers-from-email.t` | PASS | |
| `t/Email/set/create/body-structure-subparts.t` | FAIL | |
| `t/Email/set/create/body-structure.t` | FAIL | |
| `t/Email/set/create/cannot-have-blobid-and-partid.t` | PASS | |
| `t/Email/set/create/content-transfer-encoding-not-allowed.t` | FAIL | |
| `t/Email/set/create/created-response-required-properties.t` | PASS | |
| `t/Email/set/create/email-body-part-must-be-present.t` | PASS | |
| `t/Email/set/create/email-body-value-restrictions.t` | FAIL | |
| `t/Email/set/create/header-header-field-name.t` | FAIL | |
| `t/Email/set/create/headers-property-forbidden-in-body-structure.t` | FAIL | |
| `t/Email/set/create/headers-property-forbidden.t` | FAIL | |
| `t/Email/set/create/html-body.t` | FAIL | |
| `t/Email/set/create/html-body-using-blob-id.t` | FAIL | |
| `t/Email/set/create/minimum-properties-required.t` | FAIL | |
| `t/Email/set/create/no-charset-with-partid.t` | PASS | |
| `t/Email/set/create/no-content-headers-top-level.t` | FAIL | |
| `t/Email/set/create/no-date-server-provides-one.t` | FAIL | |
| `t/Email/set/create/no-duplicate-header-representations.t` | FAIL | |
| `t/Email/set/create/no-duplicate-structural-representations.t` | FAIL | |
| `t/Email/set/create/no-message-id-server-provides-one.t` | FAIL | |
| `t/Email/set/create/no-size-with-partid.t` | FAIL | |
| `t/Email/set/create/only-one-part-in-html-body.t` | FAIL | |
| `t/Email/set/create/only-one-part-in-text-body.t` | FAIL | |
| `t/Email/set/create/size-with-blob-id-must-match.t` | PASS | |
| `t/Email/set/create/text-body.t` | FAIL | |
| `t/Email/set/create/text-body-using-blob-id.t` | FAIL | |
| `t/Email/set/destroy/all-mailboxes.t` | PASS | |
| `t/Email/set/update/email-keywords.t` | FAIL | |
| `t/Email/set/update/one-folder-to-another.t` | PASS | |

### Legacy / Other Tests
| Test File | Status | Notes |
|---|---|---|
| `t/getMessages-htmlBody.t` | FAIL | |
| `t/getMessageUpdates-sinceState.t` | PASS | |
| `t/previews.t` | PASS | |
