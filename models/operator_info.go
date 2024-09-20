package models

import "github.com/Rocket-Rescue-Node/credentials"

type OperatorInfo struct {
	Timestamp        int64
	NodeID           NodeID
	OperatorType     credentials.OperatorType
	CredentialEvents []CredentialEvent
}
