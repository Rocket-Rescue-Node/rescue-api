package models

import (
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type NodeID = common.Address

// NodeRegistry contains the currently known Rocket Pool nodes.
// It is periodically updated by UpdateNodesTask.
type NodeRegistry struct {
	registry    sync.Map
	LastUpdated time.Time
}

func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{}
}

func (c *NodeRegistry) Add(id NodeID) {
	c.registry.LoadOrStore(id, struct{}{})
}

func (c *NodeRegistry) Has(id NodeID) bool {
	_, ok := c.registry.Load(id)
	return ok
}
