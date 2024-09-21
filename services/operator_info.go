package services

import (
	"fmt"
	"time"

	"github.com/Rocket-Rescue-Node/credentials"
	"github.com/Rocket-Rescue-Node/credentials/pb"
	"github.com/Rocket-Rescue-Node/rescue-api/models"
	authz "github.com/Rocket-Rescue-Node/rescue-api/models/authorization"
	"github.com/Rocket-Rescue-Node/rescue-api/util"
	"go.uber.org/zap"
)

const (
	// The maximum age for a operator info request to be considered valid.
	operatorInfoRequestMaxAge = time.Duration(15) * time.Minute
)

type OperatorInfo struct {
	CredentialEvents []int64 `json:"credentialEvents"`
	QuotaSettings    *Quota  `json:"quotaSettings,omitempty"`
}

func CreateOperatorInfo(credentialEvents []int64, quotaSettings Quota) (*OperatorInfo, error) {
	message := OperatorInfo{}
	message.CredentialEvents = credentialEvents
	message.QuotaSettings = &quotaSettings

	return &message, nil
}

func (s *Service) GetOperatorInfo(msg []byte, sig []byte, ot credentials.OperatorType) (*OperatorInfo, error) {
	var err error

	// Get node address
	nodeID, err := util.RecoverAddressFromSignature(msg, sig)
	if err != nil {
		msg := "failed to recover nodeID from signature"
		s.logger.Warn(msg, zap.Error(err))
		s.m.Counter("get_operator_info_failed_auth").Inc()
		return nil, &AuthenticationError{msg}
	}
	s.logger.Info("Recovered nodeID from signature", zap.String("nodeID", nodeID.Hex()))

	// Check if the request is fresh
	tsSecs, err := s.getTimestampFromRequest(string(msg))
	if err != nil {
		s.m.Counter("get_operator_info_invalid_timestamp").Inc()
		return nil, &ValidationError{"invalid timestamp"}
	}
	ts := time.Unix(tsSecs, 0)
	if time.Since(ts) > operatorInfoRequestMaxAge {
		s.m.Counter("get_operator_info_timestamp_too_old").Inc()
		return nil, &AuthenticationError{"timestamp is too old"}
	}

	// Check if this node is part of Rocket Pool, or a valid 0x01 credential.
	switch ot {
	case pb.OperatorType_OT_ROCKETPOOL:
		if !s.isNodeRegistered(nodeID) {
			s.m.Counter("get_operator_info_node_not_registered").Inc()
			return nil, &AuthorizationError{"node is not registered"}
		}
	case pb.OperatorType_OT_SOLO:
		if !s.enableSoloValidators {
			s.m.Counter("get_operator_info_solo_traffic_shedding").Inc()
			return nil, &AuthorizationError{"solo validators are currently not permitted"}
		}
		if !s.isWithdrawalAddress(nodeID) {
			s.m.Counter("get_operator_info_solo_not_withdrawal_address").Inc()
			return nil, &AuthorizationError{"wallet is not a withdrawal address for any validator"}
		}
	}

	// Make sure that the node is not banned from using the service.
	if !s.isNodeAuthorized(nodeID, authz.CredentialService) {
		s.m.Counter("get_operator_info_user_banned").Inc()
		return nil, &AuthorizationError{"node is not authorized"}
	}

	// Query credentials issued for this nodeID in the current window.
	now := s.clock.Now()
	currentWindowStart := now.Add(-credsQuotaWindow(ot)).Unix()

	rows, err := s.getCredEventTimestampsStmt.Query(nodeID.Bytes(), currentWindowStart, now, models.CredentialIssued, ot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Parse credential events
	var events []int64
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

	// Return empty if no cred events found
	if credCount == 0 {
		s.logger.Info(
			"No creds found for operator",
			zap.String("nodeID", nodeID.String()),
		)
		return &OperatorInfo{[]int64{}, nil}, nil
	}

	// Calculate when a new cred will be available
	quotaSettings := GetQuotaSettings(ot)

	// Create operator info
	operatorInfo, err := CreateOperatorInfo(events, *quotaSettings)
	if err != nil {
		return nil, err
	}

	s.logger.Info(
		"Retrieved operator info",
		zap.String("nodeID", nodeID.String()),
		zap.String("operatorType", ot.String()),
	)

	s.m.Counter("retrieved_operator_info").Inc()
	return operatorInfo, nil
}
