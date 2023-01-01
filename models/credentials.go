package models

import (
	"github.com/Rocket-Pool-Rescue-Node/credentials"
)

type AuthenticatedCredential = credentials.AuthenticatedCredential
type CredentialEventType int

const (
	CredentialIssued CredentialEventType = iota
	CredentialRevoked
)

type CredentialEvent struct {
	NodeID    NodeID
	Timestamp int64
	Type      CredentialEventType
}
