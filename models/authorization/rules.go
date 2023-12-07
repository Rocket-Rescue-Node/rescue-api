package authorization

import "github.com/Rocket-Rescue-Node/rescue-api/models"

type Action int

const (
	Allow Action = iota
	Deny
)

type Resource int

const (
	CredentialService Resource = iota
)

// Rule represents a rule that can be applied to Nodes while trying to access a Resource.
// Right now, only the CredentialService resource is supported.
type Rule struct {
	NodeID   models.NodeID
	Resource Resource
	Action   Action
}
