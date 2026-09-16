package jmap

import "imap-jmap/jmap/jmapcalendar"

// ShareNotificationPerson represents the changedBy entity in a ShareNotification per RFC 9670 Section 2.
type ShareNotificationPerson = jmapcalendar.ShareNotificationPerson

// ShareNotification represents a notification of a sharing change per RFC 9670 Section 2.
type ShareNotification = jmapcalendar.ShareNotification
