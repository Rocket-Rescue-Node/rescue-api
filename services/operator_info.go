package services

import (
	"fmt"

	"github.com/Rocket-Rescue-Node/credentials"
	"github.com/Rocket-Rescue-Node/rescue-api/models"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type OperatorInfo struct {
	CredentialEvents []int64 `json:"credentialEvents"`
}

func (s *Service) GetOperatorInfo(msg []byte, sig []byte, expectedNodeId common.Address, ot credentials.OperatorType) (*OperatorInfo, error) {
	var err error

	// Validate request
	nodeID, err := s.validateSignedRequest(&msg, &sig, expectedNodeId, ot)
	if err != nil {
		return nil, err
	}

	// Query credentials issued for this nodeID in the current window.
	now := s.clock.Now()
	currentWindowStart := now.Add(-credsQuotaWindow(ot)).Unix()

	rows, err := s.getCredEventTimestampsStmt.Query(nodeID.Bytes(), currentWindowStart, now.Unix(), models.CredentialIssued, ot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Parse credential events
	var events = []int64{}
	var credCount int64 = 0
	for rows.Next() {
		row_timestamp := int64(0)
		if err := rows.Scan(&row_timestamp); err != nil {
			fmt.Println("Error scanning row:", err)
			continue
		}
		events = append(events, row_timestamp)
		credCount += 1
		if credCount == credsQuota(ot) {
			break
		}
	}

	s.logger.Info(
		"Retrieved operator info",
		zap.String("nodeID", nodeID.String()),
		zap.String("operatorType", ot.String()),
	)
	s.m.Counter("retrieved_operator_info").Inc()

	return &OperatorInfo{CredentialEvents: events}, nil
}
