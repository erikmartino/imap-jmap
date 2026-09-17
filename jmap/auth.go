package jmap

import (
	"imap-jmap/jmap/jmapauth"
)

type (
	AuthCredentials           = jmapauth.AuthCredentials
	AuthBackend               = jmapauth.AuthBackend
	TokenCredentialsExtractor = jmapauth.TokenCredentialsExtractor
	AccountResolver           = jmapauth.AccountResolver
	PermissionGuard           = jmapauth.PermissionGuard
	SelfAccessGuard           = jmapauth.SelfAccessGuard
	PrimaryDomainResolver     = jmapauth.PrimaryDomainResolver
	DefaultAuthBackend        = jmapauth.DefaultAuthBackend
	defaultAuthBackend        = jmapauth.DefaultAuthBackend
)

var (
	CredentialsFromContext        = jmapauth.CredentialsFromContext
	ContextWithCredentials        = jmapauth.ContextWithCredentials
	AccountIDForSubject           = jmapauth.AccountIDForSubject
	SubjectForAccountID           = jmapauth.SubjectForAccountID
	SubjectFromContext            = jmapauth.SubjectFromContext
	ContextWithSubject            = jmapauth.ContextWithSubject
	AccountIDFromContext          = jmapauth.AccountIDFromContext
	ContextWithAccountID          = jmapauth.ContextWithAccountID
	PrincipalAccountIDFromContext = jmapauth.PrincipalAccountIDFromContext
	ContextWithPrincipalAccountID = jmapauth.ContextWithPrincipalAccountID
	DecodeBase64OrRaw             = jmapauth.DecodeBase64OrRaw
)
