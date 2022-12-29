package tasks

import (
	"time"

	"github.com/Rocket-Pool-Rescue-Node/rescue-api/external"
	"github.com/Rocket-Pool-Rescue-Node/rescue-api/models"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// UpdateNodesTask periodically updates the registry of known Rocket Pool nodes.
// It uses the Rescue Proxy (primary) and Rocketscan (fallback) APIs to retrieve the list of nodes.
type UpdateNodesTask struct {
	rescueProxyAddr string
	rocketscanURL   string
	nodes           *models.NodeRegistry
	logger          *zap.Logger
}

func NewUpdateNodesTask(proxy, rocketscan string, nodes *models.NodeRegistry, logger *zap.Logger) *UpdateNodesTask {
	return &UpdateNodesTask{
		proxy,
		rocketscan,
		nodes,
		logger,
	}
}

// updateUsingRescueProxy updates the node registry using the Rescue Proxy API.
func (t *UpdateNodesTask) updateUsingRescueProxy() error {
	src := "rescue-proxy"
	t.logger.Info("Updating Rocket Pool node registry...", zap.String("source", src))

	rescueProxyAPI := external.NewRescueProxyAPIClient(t.rescueProxyAddr)
	defer rescueProxyAPI.Close()
	nodes, err := rescueProxyAPI.GetRocketPoolNodes()
	if err != nil {
		t.logger.Warn("Failed to update node registry", zap.String("source", src), zap.Error(err))
		return err
	}
	for _, n := range nodes {
		t.nodes.Add(common.BytesToAddress(n))
	}
	t.logger.Info("Node registry successfully updated", zap.String("source", src))

	return nil
}

// updateUsingRocketscan updates the node registry using the Rocketscan API.
func (t *UpdateNodesTask) updateUsingRocketscan() error {
	src := "rocketscan"
	t.logger.Info("Updating Rocket Pool node registry...", zap.String("source", src))

	rocketscanAPI := external.NewRocketscanAPIClient(t.rocketscanURL)
	defer rocketscanAPI.Close()
	nodes, err := rocketscanAPI.GetRocketPoolNodes()
	if err != nil {
		t.logger.Warn("Failed to update node registry", zap.String("source", src), zap.Error(err))
		return err
	}
	for _, n := range nodes {
		t.nodes.Add(common.HexToAddress(n.Address))
	}
	t.logger.Info("Node registry successfully updated", zap.String("source", src))

	return nil
}

func (t *UpdateNodesTask) Run() {
	var sleepFor time.Duration
	for {
		// Try to update using the Rescue Proxy API.
		err := t.updateUsingRescueProxy()
		// If that fails, try to update using the Rocketscan API.
		if err != nil {
			err = t.updateUsingRocketscan()
		}
		if err != nil { // If both sources fail, try again quickly.
			sleepFor = time.Duration(30)
		} else { // If at least one source succeeds, sleep for a longer time.
			sleepFor = time.Duration(300)
			t.nodes.LastUpdated = time.Now()
		}
		time.Sleep(sleepFor * time.Second)
	}
}

func (t *UpdateNodesTask) Stop() error {
	return nil
}
