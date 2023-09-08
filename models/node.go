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
	registry    map[NodeID]interface{}
	lock        sync.RWMutex
	LastUpdated time.Time
}

func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{
		registry: make(map[NodeID]interface{}),
	}
}

func (c *NodeRegistry) Add(ids []NodeID) {

	c.lock.Lock()
	defer c.lock.Unlock()

	for _, id := range ids {
		c.registry[id] = struct{}{}
	}
}

func (c *NodeRegistry) Has(id NodeID) bool {

	c.lock.RLock()
	defer c.lock.RUnlock()

	_, ok := c.registry[id]
	return ok
}

func (c *NodeRegistry) Reset() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.registry = make(map[NodeID]interface{})
}
