package models

import (
	"github.com/Rocket-Rescue-Node/credentials"
)

type AuthenticatedCredential = credentials.AuthenticatedCredential
type CredentialEventType int

const (
	CredentialIssued CredentialEventType = iota
	CredentialRevoked
)

type CredentialEvent struct {
	NodeID       NodeID
	Timestamp    int64
	Type         CredentialEventType
	OperatorType credentials.OperatorType
}
