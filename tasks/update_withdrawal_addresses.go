package tasks

import (
	"time"

	"github.com/Rocket-Rescue-Node/rescue-api/external"
	"github.com/Rocket-Rescue-Node/rescue-api/models"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// UpdateWithdrawalAddressesTask periodically updates the registry of known validators' withdrawal addreses
// It uses the Rescue Proxy APIsto retrieve the list of addresses.
type UpdateWithdrawalAddressesTask struct {
	rescueProxyAddr     string
	withdrawalAddresses *models.NodeRegistry
	done                chan bool
	secureGRPC          bool
	logger              *zap.Logger
}

func NewUpdateWithdrawalAddressesTask(
	proxy string,
	withdrawalAddresses *models.NodeRegistry,
	secureGRPC bool,
	logger *zap.Logger,
) *UpdateWithdrawalAddressesTask {
	return &UpdateWithdrawalAddressesTask{
		proxy,
		withdrawalAddresses,
		make(chan bool),
		secureGRPC,
		logger,
	}
}

func (t *UpdateWithdrawalAddressesTask) updateUsingRescueProxy() error {
	t.logger.Info("Updating Withdrawal Address registry...")

	rescueProxyAPI := external.NewRescueProxyAPIClient(t.rescueProxyAddr, t.secureGRPC)
	defer rescueProxyAPI.Close()
	addresses, err := rescueProxyAPI.GetWithdrawalAddresses()
	if err != nil {
		t.logger.Warn("Failed to update Withdrawal Address registry", zap.Error(err))
		return err
	}
	newList := make([]models.NodeID, 0, len(addresses))
	for _, n := range addresses {
		newList = append(newList, common.BytesToAddress(n))
	}
	t.withdrawalAddresses.Reset()
	t.withdrawalAddresses.Add(newList)

	t.logger.Info("Withdrawal Address registry successfully updated")

	return nil
}

func (t *UpdateWithdrawalAddressesTask) Run() {
	ticker := time.NewTicker(time.Duration(1) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			t.logger.Info("Update withdrawal addresses task stopped")
			return
		case <-ticker.C:
			// Update using the Rescue Proxy API.
			err := t.updateUsingRescueProxy()
			if err != nil {
				// Try again soon
				ticker.Reset(time.Duration(30) * time.Second)
			}

			// If we succeed, try again after a longer pause
			ticker.Reset(time.Duration(300) * time.Second)
			t.withdrawalAddresses.LastUpdated = time.Now()
		}
	}
}

func (t *UpdateWithdrawalAddressesTask) Stop() error {
	t.done <- true
	return nil
}
