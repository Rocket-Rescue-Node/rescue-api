package services

import (
	"testing"

	"github.com/Rocket-Rescue-Node/credentials"
	"github.com/Rocket-Rescue-Node/rescue-api/database"
	"github.com/Rocket-Rescue-Node/rescue-api/models"
	"github.com/Rocket-Rescue-Node/rescue-api/util"
	"github.com/Rocket-Rescue-Node/rescue-proxy/metrics"
	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
)

// Create a new service using an in-memory database.
func setupTestService(t *testing.T, clock clockwork.Clock) (*Service, error) {
	var err error

	// Workaround for "no such table" errors.
	// Each connection to ":memory:" opens a brand new in-memory sql database,
	// so if the stdlib's sql engine happens to open another connection and
	// you've only specified ":memory:", that connection will see a brand new
	// database. A workaround is to use "file::memory:?cache=shared".
	// Every connection to this string will point to the same in-memory database.
	// Note that if the last database connection in the pool closes, the in-memory
	// database is deleted. Make sure the max idle connection limit is > 0, and
	// the connection lifetime is infinite.
	// Reference: https://pkg.go.dev/github.com/mattn/go-sqlite3#section-readme
	//
	// Note that this issue can also be worked around by using a single DB
	// connection, which we do in the main application for performance reasons
	// (see database.go). However, we want to use multiple connections
	// in the tests to try to catch potential concurrency issues.
	db, err := database.Open("file::memory:?cache=shared")
	if err != nil {
		return nil, err
	}
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(0)

	// Credentials.
	cm := credentials.NewCredentialManager([]byte("test"))

	// Logger
	logger, err := zap.NewDevelopmentConfig().Build()
	if err != nil {
		return nil, err
	}

	nodes := models.NewNodeRegistry()
	withdrawalAddresses := models.NewNodeRegistry()
	config := &ServiceConfig{
		DB:                   db,
		CM:                   cm,
		Nodes:                nodes,
		WithdrawalAddresses:  withdrawalAddresses,
		Logger:               logger,
		Clock:                clock,
		EnableSoloValidators: true,
	}

	_, err = metrics.Init(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		metrics.Deinit()
	})
	return NewService(config), nil
}

// Create a fake withdrawal address and add it to the list of known withdrawal addresses
func createTestWithdrawalAddress(svc *Service, register bool) (*util.Wallet, error) {
	wallet, err := util.NewWallet()
	if err != nil {
		return nil, err
	}
	if register {
		svc.withdrawalAddresses.Add([]models.NodeID{*wallet.Address})
		svc.withdrawalAddresses.LastUpdated = svc.clock.Now()
	}
	return wallet, nil
}

// Create a fake node and add it to the list of known nodes.
func createTestNode(svc *Service, register bool) (*util.Wallet, error) {
	wallet, err := util.NewWallet()
	if err != nil {
		return nil, err
	}
	if register {
		svc.nodes.Add([]models.NodeID{*wallet.Address})
		svc.nodes.LastUpdated = svc.clock.Now()
	}
	return wallet, nil
}
