package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/Rocket-Rescue-Node/credentials/pb"
	"github.com/Rocket-Rescue-Node/rescue-api/util"
	"github.com/jonboulle/clockwork"
)

func getOperatorInfo(svc *Service, node *util.Wallet) (*OperatorInfo, error) {
	var err error

	// Generate message
	var sig []byte
	msg := []byte(fmt.Sprintf("Rescue Node %d", svc.clock.Now().Unix()))
	if sig, err = node.Sign(msg); err != nil {
		return nil, fmt.Errorf("Could not sign message: %v", err)
	}

	// Get operator info
	info, err := svc.GetOperatorInfo(msg, sig, *node.Address, pb.OperatorType_OT_ROCKETPOOL)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func TestGetOperatorInfo(t *testing.T) {
	// Create a fake clock and set the validity window
	clock := clockwork.NewFakeClockAt(time.Now())

	// Create and initialize services
	svc, err := setupTestService(t, clock)
	if err != nil {
		t.Fatalf("Could not create service: %v", err)
	}
	if err = svc.Init(); err != nil {
		t.Fatalf("Could not initialize service: %v", err)
	}
	defer svc.Deinit()

	// Create a node and add it to the list of known nodes
	node, err := createTestNode(svc, true)
	if err != nil {
		t.Fatalf("Could not create node: %v", err)
	}

	// Test operator info without cred events
	i0, err := getOperatorInfo(svc, node)
	if err != nil {
		t.Fatalf("Could not get operator info: %v", err)
	}

	if len(i0.CredentialEvents) != 0 {
		t.Fatalf("Incorrect credential event count. Expected 0, got %d", len(i0.CredentialEvents))
	}

	// Create first node credential and test for matching cred event
	c1, err := createValidCredential(svc, node)
	if err != nil {
		t.Fatalf("Could not create credential: %v", err)
	}

	i1, err := getOperatorInfo(svc, node)
	if err != nil {
		t.Fatalf("Could not get operator info: %v", err)
	}
	if len(i1.CredentialEvents) != 1 {
		t.Fatalf("Incorrect credential event count. Expected 1, got %d", len(i1.CredentialEvents))
	}

	// Make sure timestamp matches
	if i1.CredentialEvents[0] != c1.Credential.Timestamp {
		t.Fatalf("Incorrect credential timestamp. Expected %d got: %d", c1.Credential.Timestamp, i1.CredentialEvents[0])
	}

	// Advance the clock just before credsMinValidityWindow expires.
	clock.Advance(AuthValidityWindow(pb.OperatorType_OT_ROCKETPOOL) - credsMinValidityWindow - 1*time.Second)
	// Make sure that the node registry is considered up-to-date.
	svc.nodes.LastUpdated = svc.clock.Now()
	// Check that the reissued credential matches expected values
	c1Reissued, err := createValidCredential(svc, node)
	if err != nil {
		t.Fatalf("Could not create credential: %v", err)
	}
	if c1Reissued.Credential.Timestamp != i1.CredentialEvents[len(i1.CredentialEvents)-1] {
		t.Fatalf("Reissued credentials do not match (%d != %d)",
			c1Reissued.Credential.Timestamp, i1.CredentialEvents[len(i1.CredentialEvents)-1])
	}

	// Create up to the maximum number of credentials, advancing the clock
	// by authValidityWindow each time, and making sure that new info is retrieved
	prevInfo := i1
	for i := 1; i < int(quotas[pb.OperatorType_OT_ROCKETPOOL].count); i++ {
		clock.Advance(AuthValidityWindow(pb.OperatorType_OT_ROCKETPOOL))
		svc.nodes.LastUpdated = svc.clock.Now()
		_, err = createValidCredential(svc, node)
		if err != nil {
			t.Fatalf("Could not create credential: %v", err)
		}
		info, err := getOperatorInfo(svc, node)
		if err != nil {
			t.Fatalf("Could not get operator info: %v", err)
		}
		if len(info.CredentialEvents) != i+1 {
			t.Fatalf("Incorrect credential event count. Expected %d, got %d", i+1, len(info.CredentialEvents))
		}
		if prevInfo.CredentialEvents[0] == info.CredentialEvents[0] {
			t.Fatalf("Operator info should not match (%d == %d)",
				prevInfo.CredentialEvents[0], info.CredentialEvents[0])
		}
		prevInfo = info
	}
	i2 := prevInfo

	// Advance the clock just enough so that the oldest credential is not within
	// credsQuotaWindow anymore. This should decrease cred event count by 1.
	if len(i2.CredentialEvents) != 4 {
		t.Fatalf("Started with incorrect cred count. Expected 4, got %d", len(i2.CredentialEvents))
	}
	i2InfoQuotaExpiry := time.Unix(i2.CredentialEvents[len(i2.CredentialEvents)-1], 0).Add(quotas[pb.OperatorType_OT_ROCKETPOOL].window)
	clock.Advance(i2InfoQuotaExpiry.Sub(clock.Now()))
	svc.nodes.LastUpdated = svc.clock.Now()
	i3, err := getOperatorInfo(svc, node)
	if err != nil {
		t.Fatalf("Could not get operator info: %v", err)
	}
	if len(i3.CredentialEvents) != 3 {
		t.Fatalf("Incorrect credential event count. Expected 3, got %d", len(i3.CredentialEvents))
	}
}
