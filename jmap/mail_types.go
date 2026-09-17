package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

type (
	MailboxRights           = jmapmail.MailboxRights
	Mailbox                 = jmapmail.Mailbox
	Thread                  = jmapmail.Thread
	EmailAddress            = jmapmail.EmailAddress
	EmailHeader             = jmapmail.EmailHeader
	EmailBodyPart           = jmapmail.EmailBodyPart
	EmailBodyValue          = jmapmail.EmailBodyValue
	SmimeVerificationResult = jmapmail.SmimeVerificationResult
	Email                   = jmapmail.Email
	Identity                = jmapmail.Identity
	SubmissionAddress       = jmapmail.SubmissionAddress
	SubmissionEnvelope      = jmapmail.SubmissionEnvelope
	DeliveryStatus          = jmapmail.DeliveryStatus
	DSNParameters           = jmapmail.DSNParameters
	MDNParameters           = jmapmail.MDNParameters
	EmailSubmission         = jmapmail.EmailSubmission
	SearchSnippet           = jmapmail.SearchSnippet
	VacationResponse        = jmapmail.VacationResponse
)
