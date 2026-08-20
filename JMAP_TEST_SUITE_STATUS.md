# JMAP-TestSuite Conformance Status

This document tracks the status of running the official Fastmail `JMAP-TestSuite` (`~/git/fastmail/JMAP-TestSuite`) against `imap-jmap`.

## Summary
- **Total Test Files**: 89
- **Passing**: 89 (100%)
- **Failing**: 0 (0%)

---

## Detailed Status by Subsystem

### Core & Transport
| Test File | Status | Notes |
|---|---|---|
| `t/basic.t` | PASS | Basic account, mailbox creation, upload, import |
| `t/core/backrefs-simple.t` | PASS | Result reference resolution |
| `t/core/download.t` | PASS | Blob download, content types, byte-range requests |
| `t/core/jmap-session-resource.t` | PASS | Session object, capabilities, accounts |
| `t/core/upload.t` | PASS | Blob upload, size, type |

### Mailbox Subsystem
| Test File | Status | Notes |
|---|---|---|
| `t/Mailbox/changes/changed-properties.t` | PASS | Property change tracking |
| `t/Mailbox/changes/max-changes-has-more-changes.t` | PASS | Max changes & hasMoreChanges |
| `t/Mailbox/changes/no-changes.t` | PASS | Correct empty state when unchanged |
| `t/Mailbox/changes/with-changes.t` | PASS | Created, updated, destroyed tracking |
| `t/Mailbox/get/limiting-properties-in-response.t` | PASS | Property projection |
| `t/Mailbox/get/no-existing-entities.t` | PASS | Empty mailbox queries |
| `t/Mailbox/get/some-entities.t` | PASS | Fetching multiple mailboxes |
| `t/Mailbox/query/filtering-with-filter-conditions.t` | PASS | Role, parentId, name filters |
| `t/Mailbox/query/filtering-with-filter-operators.t` | PASS | AND / OR / NOT filter combinations |
| `t/Mailbox/query/no-existing-entities.t` | PASS | Empty query results |
| `t/Mailbox/query/sorting-and-limiting.t` | PASS | Sort keys & limit/position |
| `t/Mailbox/set/create/all-settable-fields-provided.t` | PASS | Creating mailbox with all fields |
| `t/Mailbox/set/create/defaults-omitted.t` | PASS | Default values assignment |
| `t/Mailbox/set/create/immutable-fields.t` | PASS | Immutable field enforcement |
| `t/Mailbox/set/destroy/good-destroy-no-messages.t` | PASS | Clean mailbox destroy |
| `t/Mailbox/set/destroy/mailbox-has-child-error.t` | PASS | mailboxHasChild rejection |
| `t/Mailbox/set/destroy/mailbox-has-email-error.t` | PASS | mailboxHasEmail rejection |
| `t/Mailbox/set/destroy/on-destroy-remove-messages-true-mail-in-other-boxes.t` | PASS | onDestroyRemoveEmails handling |
| `t/Mailbox/set/destroy/on-destroy-remove-messages-true-mail-only-here.t` | PASS | onDestroyRemoveEmails destroy |
| `t/Mailbox/set/update/basic.t` | PASS | Mailbox property patching |

### Thread Subsystem
| Test File | Status | Notes |
|---|---|---|
| `t/Thread/changes/changes.t` | PASS | Thread change tracking |
| `t/Thread/changes/max-changes-has-more-changes.t` | PASS | Max changes & hasMoreChanges |
| `t/Thread/changes/no-changes.t` | PASS | Unchanged thread states |
| `t/Thread/get/empty-list.t` | PASS | Empty list retrieval |
| `t/Thread/get/few-messages.t` | PASS | Thread email aggregation |
| `t/Thread/get/unknown-ids.t` | PASS | notFound reporting |

### Email Subsystem
| Test File | Status | Notes |
|---|---|---|
| `t/Email/changes/changes.t` | PASS | Email change tracking |
| `t/Email/changes/max-changes-has-more-changes.t` | PASS | Max changes & hasMoreChanges |
| `t/Email/changes/no-changes.t` | PASS | Unchanged email states |
| `t/Email/get/attachments.t` | PASS | Attachment metadata and blobs |
| `t/Email/get/body-properties.t` | PASS | Body property parsing & projection |
| `t/Email/get/body-structure-and-attachments.t` | PASS | Multipart structure & attachments |
| `t/Email/get/fetch-all-body-values.t` | PASS | fetchAllBodyValues support |
| `t/Email/get/fetch-html-body-values.t` | PASS | fetchHTMLBodyValues support |
| `t/Email/get/fetch-text-body-values.t` | PASS | fetchTextBodyValues support |
| `t/Email/get/has-attachment.t` | PASS | hasAttachment calculation |
| `t/Email/get/header-header-field-name.t` | PASS | Form-specific header parsing |
| `t/Email/get/html-body.t` | PASS | HTML part extraction |
| `t/Email/get/max-body-value-bytes.t` | PASS | maxBodyValueBytes truncation |
| `t/Email/get/no-ids.t` | PASS | Empty list retrieval |
| `t/Email/get/properties.t` | PASS | Property projection |
| `t/Email/get/text-body.t` | PASS | Text part extraction |
| `t/Email/import/bad-values.t` | PASS | Import validation & error types |
| `t/Email/import/good-imports.t` | PASS | Clean email import round-trip |
| `t/Email/import/invalid-email.t` | PASS | RFC 5322 syntax validation |
| `t/Email/import/one-fails-another-succeeds.t` | PASS | Partial batch success |
| `t/Email/queryChanges/simple.t` | PASS | QueryChanges added/removed diff |
| `t/Email/query/filtering.t` | PASS | Search filters, keywords, thread ctx |
| `t/Email/query/no-existing-entities.t` | PASS | Empty query results |
| `t/Email/set/create/attachments.t` | PASS | Creating with attachments |
| `t/Email/set/create/attachments-using-blob-id.t` | PASS | Attachments with blobId references |
| `t/Email/set/create/blob-not-found.t` | PASS | Missing blob error handling |
| `t/Email/set/create/body-structure-cannot-duplicate-headers-from-email.t` | PASS | Duplicate headers validation |
| `t/Email/set/create/body-structure-subparts.t` | PASS | Nested multipart subparts |
| `t/Email/set/create/body-structure.t` | PASS | bodyStructure creation |
| `t/Email/set/create/cannot-have-blobid-and-partid.t` | PASS | Mutually exclusive blobId/partId validation |
| `t/Email/set/create/content-transfer-encoding-not-allowed.t` | PASS | CTE header rejection |
| `t/Email/set/create/created-response-required-properties.t` | PASS | Response shape strictness |
| `t/Email/set/create/email-body-part-must-be-present.t` | PASS | Part presence validation |
| `t/Email/set/create/email-body-value-restrictions.t` | PASS | Value restrictions |
| `t/Email/set/create/header-header-field-name.t` | PASS | Form-specific header parsing in create |
| `t/Email/set/create/headers-property-forbidden-in-body-structure.t` | PASS | Forbidden headers property validation |
| `t/Email/set/create/headers-property-forbidden.t` | PASS | Forbidden headers property validation |
| `t/Email/set/create/html-body.t` | PASS | HTML part creation |
| `t/Email/set/create/html-body-using-blob-id.t` | PASS | HTML parts with blobId references |
| `t/Email/set/create/minimum-properties-required.t` | PASS | Minimal valid Email creation |
| `t/Email/set/create/no-charset-with-partid.t` | PASS | Charset defaulting |
| `t/Email/set/create/no-content-headers-top-level.t` | PASS | Content-* header validation |
| `t/Email/set/create/no-date-server-provides-one.t` | PASS | Server sentAt timestamp assignment |
| `t/Email/set/create/no-duplicate-header-representations.t` | PASS | Duplicate header representations rejection |
| `t/Email/set/create/no-duplicate-structural-representations.t` | PASS | Duplicate structural representations rejection |
| `t/Email/set/create/no-message-id-server-provides-one.t` | PASS | Server Message-ID generation |
| `t/Email/set/create/no-size-with-partid.t` | PASS | Size calculation from bodyValues |
| `t/Email/set/create/only-one-part-in-html-body.t` | PASS | HTML single part validation |
| `t/Email/set/create/only-one-part-in-text-body.t` | PASS | Text single part validation |
| `t/Email/set/create/size-with-blob-id-must-match.t` | PASS | Size validation against blob |
| `t/Email/set/create/text-body.t` | PASS | Text part creation |
| `t/Email/set/create/text-body-using-blob-id.t` | PASS | Text parts with blobId references |
| `t/Email/set/destroy/all-mailboxes.t` | PASS | Destroy email from all mailboxes |
| `t/Email/set/update/email-keywords.t` | PASS | Patching email keywords |
| `t/Email/set/update/one-folder-to-another.t` | PASS | Moving email between mailboxes |

### Legacy / Other Tests
| Test File | Status | Notes |
|---|---|---|
| `t/getMessages-htmlBody.t` | PASS | HTML body extraction compatibility |
| `t/getMessageUpdates-sinceState.t` | PASS | Change tracking compatibility |
| `t/previews.t` | PASS | Preview generation |

