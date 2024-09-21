package services

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Rocket-Rescue-Node/credentials"
	"github.com/Rocket-Rescue-Node/credentials/pb"
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

func TestCreateCredentialLifecycle(t *testing.T) {
	// Create a fake clock and set the validity window.
	clock := clockwork.NewFakeClockAt(time.Now())
	// Create and initialize services.
	svc, err := setupTestService(t, clock)
	if err != nil {
		t.Fatalf("Could not create service: %v", err)
	}
	if err = svc.Init(); err != nil {
		t.Fatalf("Could not initialize service: %v", err)
	}
	defer svc.Deinit()

	// Create a node and add it to the list of known nodes.
	node, err := createTestNode(svc, true)
	if err != nil {
		t.Fatalf("Could not create node: %v", err)
	}

	// First node credential.
	c0, err := createValidCredential(svc, node)
	if err != nil {
		t.Fatalf("Could not create credential: %v", err)
	}
	// Advance the clock just before credsMinValidityWindow expires.
	clock.Advance(AuthValidityWindow(pb.OperatorType_OT_ROCKETPOOL) - credsMinValidityWindow - 1*time.Second)
	// Make sure that the node registry is considered up-to-date.
	svc.nodes.LastUpdated = svc.clock.Now()
	// Check that the credential c0 is reused, since it is still valid and
	// within the minValidityWindow.
	c0Reissued, err := createValidCredential(svc, node)
	if err != nil {
		t.Fatalf("Could not create credential: %v", err)
	}
	if c0.Credential.Timestamp != c0Reissued.Credential.Timestamp {
		t.Fatalf("Credentials do not match (%d != %d)",
			c0.Credential.Timestamp, c0Reissued.Credential.Timestamp)
	}

	// Second node credential.
	// Advance the clock past minValidityWindow. This should cause a new credential
	// to be created, even though the current one is still valid for a few days.
	clock.Advance(2 * time.Second)
	svc.nodes.LastUpdated = svc.clock.Now()
	c1, err := createValidCredential(svc, node)
	if err != nil {
		t.Fatalf("Could not create credential: %v", err)
	}
	if c0.Credential.Timestamp == c1.Credential.Timestamp {
		t.Fatalf("Credentials should not match (%d == %d)",
			c0.Credential.Timestamp, c1.Credential.Timestamp)
	}

	// Create up to the maximum number of credentials, advancing the clock
	// by authValidityWindow each time, and making sure that new credentials are
	// created each time.
	prevCred := c1
	for i := 2; i < int(quotas[pb.OperatorType_OT_ROCKETPOOL].Count); i++ {
		clock.Advance(AuthValidityWindow(pb.OperatorType_OT_ROCKETPOOL))
		svc.nodes.LastUpdated = svc.clock.Now()
		cred, err := createValidCredential(svc, node)
		if err != nil {
			t.Fatalf("Could not create credential: %v", err)
		}
		if prevCred.Credential.Timestamp == cred.Credential.Timestamp {
			t.Fatalf("Credentials should not match (%d == %d)",
				prevCred.Credential.Timestamp, cred.Credential.Timestamp)
		}
		prevCred = cred
	}

	// Advance the clock just before the credential expires.
	// This should cause the credential to be reused, even though it is
	// older than minValidityWindow, because we have exhausted credsQuota.
	clock.Advance(AuthValidityWindow(pb.OperatorType_OT_ROCKETPOOL) - 1*time.Second)
	svc.nodes.LastUpdated = svc.clock.Now()
	cred, err := createValidCredential(svc, node)
	if err != nil {
		t.Fatalf("Could not create credential: %v", err)
	}
	if cred.Credential.Timestamp != prevCred.Credential.Timestamp {
		t.Fatalf("Credentials do not match (%d != %d)",
			cred.Credential.Timestamp, prevCred.Credential.Timestamp)
	}

	// At this point, the credential quota is exhausted.
	// Make sure we cannot create more credentials.
	clock.Advance(2 * time.Second)
	svc.nodes.LastUpdated = svc.clock.Now()
	_, err = createValidCredential(svc, node)
	if !errors.Is(err, &AuthorizationError{}) {
		t.Fatalf("Expected AuthorizationError, got %v", err)
	}

	// Advance the clock just enough so that the oldest credential is not within
	// credsQuotaWindow anymore. This should increase the available quota to 1,
	// and allow us to create a new credential.
	c0QuotaExpiry := time.Unix(c0.Credential.Timestamp, 0).Add(quotas[pb.OperatorType_OT_ROCKETPOOL].Window)
	clock.Advance(c0QuotaExpiry.Sub(clock.Now()))

	svc.nodes.LastUpdated = svc.clock.Now()
	cred, err = createValidCredential(svc, node)
	if err != nil {
		t.Fatalf("Could not create credential: %v", err)
	}
	if cred.Credential.Timestamp == prevCred.Credential.Timestamp {
		t.Fatalf("Credentials should not match (%d == %d)",
			cred.Credential.Timestamp, prevCred.Credential.Timestamp)
	}
}

func createValidCredential(svc *Service, node *util.Wallet) (*credentials.AuthenticatedCredential, error) {
	var err error

	// Create a credentials request.
	var sig []byte
	msg := []byte(fmt.Sprintf("Rescue Node %d", svc.clock.Now().Unix()))
	if sig, err = node.Sign(msg); err != nil {
		return nil, fmt.Errorf("Could not sign message: %v", err)
	}
	// Create credential.
	cred, err := svc.CreateCredentialWithRetry(msg, sig, pb.OperatorType_OT_ROCKETPOOL)
	if err != nil {
		return nil, err
	}
	// Check credential.
	if cred == nil || cred.Credential == nil {
		return nil, fmt.Errorf("Credential should not be nil")
	}
	// Check that the credential has a valid HMAC.
	if _, err = svc.cm.Verify(cred); err != nil {
		return nil, fmt.Errorf("Credential HMAC is invalid: %v", err)
	}
	// Make sure the node ID matches the node address.
	if !bytes.Equal(cred.Credential.NodeId, node.Address.Bytes()) {
		err := fmt.Errorf("Credential node ID does not match node address (%x != %x)",
			cred.Credential.NodeId, node.Address.Bytes())
		return nil, err
	}

	return cred, nil
}

func TestCreateCredentialRequests(t *testing.T) {
	// Create a fake clock and set the validity window.
	clock := clockwork.NewFakeClockAt(time.Now())
	// Create and initialize services.
	svc, err := setupTestService(t, clock)
	if err != nil {
		t.Fatalf("Could not create service: %v", err)
	}
	if err = svc.Init(); err != nil {
		t.Fatalf("Could not initialize service: %v", err)
	}
	defer svc.Deinit()

	// Create a node and add it to the list of known nodes.
	node, err := createTestNode(svc, true)
	if err != nil {
		t.Fatalf("Could not create node: %v", err)
	}

	// Create a solo validator cred and add it to the list of know withdrawal addresses.
	withdrawalAddress, err := createTestWithdrawalAddress(svc, true)
	if err != nil {
		t.Fatalf("Could not create withdrawal address: %v", err)
	}

	// Valid message and signature.
	msg := []byte(fmt.Sprintf("Rescue Node %d", svc.clock.Now().Unix()))
	sig, err := node.Sign(msg)
	if err != nil {
		t.Fatalf("Could not sign message: %v", err)
	}

	// Solo validator signature
	soloSig, err := withdrawalAddress.Sign(msg)
	if err != nil {
		t.Fatalf("Could not sign message: %v", err)
	}

	// Invalid signature.
	invalidSig := []byte{0xff, 0xff, 0xff, 0xff}
	copy(invalidSig[4:], sig[4:])

	// Malformed timestamp.
	badMsg := []byte("Rescue Node [TIME]")
	badMsgSig, err := node.Sign(badMsg)
	if err != nil {
		t.Fatalf("Could not sign message: %v", err)
	}

	// Expired timestamp.
	oldMsg := []byte("Rescue Node 0")
	oldMsgSig, err := node.Sign(oldMsg)
	if err != nil {
		t.Fatalf("Could not sign message: %v", err)
	}

	// Request from a node not part of Rocket Pool.
	otherNode, err := createTestNode(svc, false)
	if err != nil {
		t.Fatalf("Could not create node: %v", err)
	}
	otherMsg := []byte(fmt.Sprintf("Rescue Node %d", svc.clock.Now().Unix()))
	otherSig, err := otherNode.Sign(otherMsg)
	if err != nil {
		t.Fatalf("Could not sign message: %v", err)
	}

	// Setup test data.
	data := []struct {
		name string
		msg  []byte
		sig  []byte
		ot   credentials.OperatorType
		err  error
	}{
		{"valid", msg, sig, pb.OperatorType_OT_ROCKETPOOL, nil},
		{"valid_solo", msg, soloSig, pb.OperatorType_OT_SOLO, nil},
		{"solo_masquerading_rp", msg, soloSig, pb.OperatorType_OT_ROCKETPOOL, &AuthorizationError{}},
		{"rp_masquerading_solo", msg, sig, pb.OperatorType_OT_SOLO, &AuthorizationError{}},
		{"malformed_signature", msg, []byte("invalid"), pb.OperatorType_OT_ROCKETPOOL, &AuthenticationError{}},
		{"invalid_signature", msg, invalidSig, pb.OperatorType_OT_ROCKETPOOL, &AuthenticationError{}},
		{"malformed_message", badMsg, badMsgSig, pb.OperatorType_OT_ROCKETPOOL, &ValidationError{}},
		{"expired_timestamp", oldMsg, oldMsgSig, pb.OperatorType_OT_ROCKETPOOL, &ValidationError{}},
		{"empty_message", []byte{}, sig, pb.OperatorType_OT_ROCKETPOOL, &ValidationError{}},
		{"empty_signature", msg, []byte{}, pb.OperatorType_OT_ROCKETPOOL, &AuthenticationError{}},
		{"unknown_node", otherMsg, otherSig, pb.OperatorType_OT_ROCKETPOOL, &AuthorizationError{}},
	}

	for _, d := range data {
		t.Run(d.name, func(t *testing.T) {
			_, err := svc.CreateCredentialWithRetry(d.msg, d.sig, d.ot)
			if !errors.Is(err, d.err) {
				t.Fatalf("Expected error %v, got %v", d.err, err)
			}
		})
	}
}

// Launch a few goroutines to create credentials concurrently.
// This test is to try to hash out any concurrency issues.
func TestCreateCredentialConcurrent(t *testing.T) {
	// Create a fake clock and set the validity window.
	clock := clockwork.NewRealClock()
	// Create and initialize services.
	svc, err := setupTestService(t, clock)
	if err != nil {
		t.Fatalf("Could not create service: %v", err)
	}
	if err = svc.Init(); err != nil {
		t.Fatalf("Could not initialize service: %v", err)
	}
	defer svc.Deinit()

	// Create nodes and add them to the node registry.
	numNodes := 10000
	nodes := make([]*util.Wallet, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[i], err = createTestNode(svc, true)
		if err != nil {
			t.Fatalf("Could not create node: %v", err)
		}
	}

	// Launch a few goroutines to create credentials concurrently.
	var wg sync.WaitGroup
	errChan := make(chan error, numNodes)
	numGoRoutines := 10
	credsPerGoRoutine := numNodes / numGoRoutines
	createCreds := func(id int, t *testing.T, count int) {
		// Calculate the range of nodes to create credentials for.
		start := id * count
		end := start + count
		defer wg.Done()
		for i := start; i < end; i++ {
			msg := []byte(fmt.Sprintf("Rescue Node %d", svc.clock.Now().Unix()))
			sig, err := nodes[i].Sign(msg)
			if err != nil {
				t.Errorf("Could not sign message: %v", err)
				errChan <- err
				return
			}
			_, err = svc.CreateCredentialWithRetry(msg, sig, pb.OperatorType_OT_ROCKETPOOL)
			if err != nil {
				t.Errorf("Could not create credential %d: %v", i, err)
				errChan <- err
				return
			}
		}
	}

	for id := 0; id < numGoRoutines; id++ {
		wg.Add(1)
		go createCreds(id, t, credsPerGoRoutine)
	}

	wg.Wait()

	// Check for errors.
	if len(errChan) > 0 {
		t.Fatalf("Received errors: %d", len(errChan))
	}
}
