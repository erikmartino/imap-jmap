package jmap

// This file re-exports all sieve domain types from the jmapsieve sub-package
// as type aliases, preserving full backward compatibility for all existing callers.

import "imap-jmap/jmap/jmapsieve"

// SieveScript represents a SieveScript object per RFC 9661 Section 1.4.
type SieveScript = jmapsieve.SieveScript
