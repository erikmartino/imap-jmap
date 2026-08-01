# Agent Guidelines & Project Rules

## RFC Validation Requirement
All new features, data model projections, protocol mappers, and payload transformations MUST be strictly validated against official IETF RFC standards.

Before implementing or modifying any JMAP methods, objects, or patches, inspect the corresponding RFC specifications to ensure full spec compliance (including data types, object maps, JSON Pointers, and capability declarations).

### Official Specification References:

#### Core JMAP Specifications
- **JMAP Core**: [RFC 8620](https://www.rfc-editor.org/rfc/rfc8620.html) — *The JSON Meta Application Protocol (JMAP)*
- **JMAP Mail**: [RFC 8621](https://www.rfc-editor.org/rfc/rfc8621.html) — *The JSON Meta Application Protocol (JMAP) for Mail*
- **JMAP WebSockets**: [RFC 8887](https://www.rfc-editor.org/rfc/rfc8887.html) — *JMAP Subprotocol for WebSocket*
- **JMAP MDN**: [RFC 9007](https://www.rfc-editor.org/rfc/rfc9007.html) — *Message Disposition Notifications in JMAP*
- **JMAP S/MIME**: [RFC 9219](https://www.rfc-editor.org/rfc/rfc9219.html) — *S/MIME Signature Verification Extension*
- **JMAP Blob Management**: [RFC 9404](https://www.rfc-editor.org/rfc/rfc9404.html) — *JMAP Blob Management Extension*
- **JMAP Quotas**: [RFC 9425](https://www.rfc-editor.org/rfc/rfc9425.html) — *JMAP for Quotas*
- **JMAP for Contacts**: [RFC 9610](https://www.rfc-editor.org/rfc/rfc9610.html) — *JMAP for Contacts*
- **JMAP for Sieve Scripts**: [RFC 9661](https://www.rfc-editor.org/rfc/rfc9661.html) — *JMAP for Sieve Scripts*
- **JMAP Sharing**: [RFC 9670](https://www.rfc-editor.org/rfc/rfc9670.html) — *JMAP Sharing*
- **JMAPACCESS (IMAP)**: [RFC 9698](https://www.rfc-editor.org/rfc/rfc9698.html) — *JMAPACCESS Extension for IMAP*
- **JMAP Push VAPID**: [RFC 9749](https://www.rfc-editor.org/rfc/rfc9749.html) — *VAPID Identification in JMAP Web Push*
- **JMAP Keywords & Attributes**: [RFC 9979](https://www.rfc-editor.org/rfc/rfc9979.html) — *IMAP/JMAP Keywords and Mailbox Name Attributes*

#### Data Representation Specifications
- **JSContact (Card Specification)**: [RFC 9553](https://www.rfc-editor.org/rfc/rfc9553.html) — *JSContact: A JSON Representation of Contact Data*
- **JSCalendar (Calendar Specification)**: [RFC 8984](https://www.rfc-editor.org/rfc/rfc8984.html) — *JSCalendar: A JSON Representation of Calendar Data*

#### CardDAV & CalDAV Protocol Specifications
- **CardDAV**: [RFC 6352](https://www.rfc-editor.org/rfc/rfc6352.html) — *CardDAV: vCard Extensions to WebDAV*
- **CalDAV**: [RFC 4791](https://www.rfc-editor.org/rfc/rfc4791.html) — *CalDAV: Calendaring Extensions to WebDAV*
- **vCard 4.0**: [RFC 6350](https://www.rfc-editor.org/rfc/rfc6350.html) — *vCard Format Specification*
- **vCard 3.0**: [RFC 2426](https://www.rfc-editor.org/rfc/rfc2426.html) — *vCard MIME Directory Profile*
- **iCalendar**: [RFC 5545](https://www.rfc-editor.org/rfc/rfc5545.html) — *Internet Calendaring and Email Object Specification*
- **iTIP (Scheduling)**: [RFC 5546](https://www.rfc-editor.org/rfc/rfc5546.html) — *iCalendar Transport-Independent Interoperability Protocol*
- **iMIP (Email Scheduling)**: [RFC 6047](https://www.rfc-editor.org/rfc/rfc6047.html) — *Message Binding for iTIP*

#### IMAP Protocol & Message Specifications
- **IMAP4rev1**: [RFC 3501](https://www.rfc-editor.org/rfc/rfc3501.html) — *INTERNET MESSAGE ACCESS PROTOCOL - VERSION 4rev1*
- **IMAP4rev2**: [RFC 9051](https://www.rfc-editor.org/rfc/rfc9051.html) — *Internet Message Access Protocol (IMAP) - Version 4rev2*
- **IMAP CONDSTORE & QRESYNC**: [RFC 7162](https://www.rfc-editor.org/rfc/rfc7162.html) — *IMAP Extensions: Quick Mailbox Resynchronization (QRESYNC) and Conditional STORE (CONDSTORE)*
- **IMAP IDLE**: [RFC 2177](https://www.rfc-editor.org/rfc/rfc2177.html) — *IMAP4 IDLE command*
- **IMAP MOVE**: [RFC 6851](https://www.rfc-editor.org/rfc/rfc6851.html) — *Internet Message Access Protocol (IMAP) - MOVE Extension*
- **IMAP SPECIAL-USE**: [RFC 6154](https://www.rfc-editor.org/rfc/rfc6154.html) — *IMAP LIST Extension for Special-Use Mailboxes*
- **IMAP UIDPLUS**: [RFC 4315](https://www.rfc-editor.org/rfc/rfc4315.html) — *Internet Message Access Protocol (IMAP) - UIDPLUS extension*
- **Internet Message Format**: [RFC 5322](https://www.rfc-editor.org/rfc/rfc5322.html) — *Internet Message Format*
- **MIME Media Types**: [RFC 2045](https://www.rfc-editor.org/rfc/rfc2045.html) — *Multipurpose Internet Mail Extensions (MIME) Part One*

