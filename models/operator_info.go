package models

type OperatorInfo struct {
	CredentialEvents []int64 `json:"credentialEvents"`
	NextCred         int64   `json:"nextCred"`
}
