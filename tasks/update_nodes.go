package tasks

import (
	"time"

	"github.com/Rocket-Rescue-Node/rescue-api/external"
	"github.com/Rocket-Rescue-Node/rescue-api/models"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// UpdateNodesTask periodically updates the registry of known Rocket Pool nodes.
// It uses the Rescue Proxy to retrieve the list of nodes.
type UpdateNodesTask struct {
	rescueProxyAddr string
	nodes           *models.NodeRegistry
	done            chan bool
	secureGRPC      bool
	logger          *zap.Logger
}

func NewUpdateNodesTask(
	proxy string,
	nodes *models.NodeRegistry,
	secureGRPC bool,
	logger *zap.Logger,
) *UpdateNodesTask {
	return &UpdateNodesTask{
		proxy,
		nodes,
		make(chan bool),
		secureGRPC,
		logger,
	}
}

// updateUsingRescueProxy updates the node registry using the Rescue Proxy API.
func (t *UpdateNodesTask) updateUsingRescueProxy() error {
	src := "rescue-proxy"
	t.logger.Info("Updating Rocket Pool node registry...", zap.String("source", src))

	rescueProxyAPI := external.NewRescueProxyAPIClient(t.logger, t.rescueProxyAddr, t.secureGRPC)
	defer rescueProxyAPI.Close()
	nodes, err := rescueProxyAPI.GetRocketPoolNodes()
	if err != nil {
		t.logger.Warn("Failed to update node registry", zap.String("source", src), zap.Error(err))
		return err
	}
	newList := make([]models.NodeID, 0, len(nodes))
	for _, n := range nodes {
		newList = append(newList, common.BytesToAddress(n))
	}
	t.nodes.Add(newList)

	t.logger.Info("Node registry successfully updated", zap.String("source", src))

	return nil
}

func (t *UpdateNodesTask) Run() {
	ticker := time.NewTicker(time.Duration(1) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			t.logger.Info("Update nodes task stopped")
			return
		case <-ticker.C:
			// Try to update using the Rescue Proxy API.
			err := t.updateUsingRescueProxy()
			if err != nil { // If sources fail, try again quickly.
				ticker.Reset(time.Duration(30) * time.Second)
			}
			// Otherwise, wait longer
			t.nodes.LastUpdated = time.Now()
			ticker.Reset(time.Duration(300) * time.Second)
		}
	}
}

func (t *UpdateNodesTask) Stop() error {
	t.done <- true
	return nil
}
