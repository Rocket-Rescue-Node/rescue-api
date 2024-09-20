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

func CreateCredentialEvent(timestamp time.Time, nodeID []byte, credType models.CredentialEventType, operatorType credentials.OperatorType) (*models.CredentialEvent, error) {
	if len(nodeID) != 20 {
		return nil, fmt.Errorf("invalid nodeID length. Expected 20, got %d", len(nodeID))
	}
	message := models.CredentialEvent{}
	message.Timestamp = timestamp.Unix()
	message.NodeID.SetBytes(nodeID)
	message.Type = credType
	message.OperatorType = operatorType

	return &message, nil
}

func CreateOperatorInfo(timestamp time.Time, nodeID []byte, operatorType credentials.OperatorType, credentialEvents []models.CredentialEvent) (*models.OperatorInfo, error) {
	if len(nodeID) != 20 {
		return nil, fmt.Errorf("invalid nodeID length. Expected 20, got %d", len(nodeID))
	}
	message := models.OperatorInfo{}
	message.Timestamp = timestamp.Unix()
	message.NodeID.SetBytes(nodeID)
	message.OperatorType = operatorType
	message.CredentialEvents = credentialEvents

	return &message, nil
}

// Sno: GetOperatorInfoWithRetry()?

func (s *Service) GetOperatorInfo(msg []byte, sig []byte, ot credentials.OperatorType) (*models.OperatorInfo, error) {
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

	// Sno: Is it necessary to use the locking logic for this?
	// Start a transaction
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	// Query credentials issued for this nodeID in the current window.
	now := s.clock.Now()
	currentWindowStart := now.Add(-credsQuotaWindow(ot)).Unix()
	gacs := tx.Stmt(s.getAllCredEventsStmt)
	defer gacs.Close()
	rows, err := gacs.Query(nodeID.Bytes(), currentWindowStart, now, models.CredentialIssued, ot)
	if err != nil {
		return nil, err
	}

	// Parse credential events
	var events []models.CredentialEvent
	var credCount int64 = 0
	for rows.Next() {
		row_nodeID, row_timestamp, row_credType, row_operatorType := []byte{}, int64(0), 0, 0
		if err := rows.Scan(&row_nodeID, &row_timestamp, &row_credType, &row_operatorType); err != nil {
			fmt.Println("Error scanning row:", err)
			continue
		}

		event, err := CreateCredentialEvent(time.Unix(row_timestamp, 0), row_nodeID, models.CredentialEventType(row_credType), pb.OperatorType(row_operatorType))
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
		credCount += 1
		if credCount == credsQuota(ot) {
			break
		}
	}

	// Create operator info
	operatorInfo, err := CreateOperatorInfo(now, nodeID.Bytes(), ot, events)
	if err != nil {
		return nil, err
	}

	// Commit the transaction.
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.logger.Info(
		"Retrieved operator info",
		zap.String("nodeID", string(nodeID.Bytes())),
		zap.String("operatorType", ot.String()),
	)

	s.m.Counter("retrieved_operator_info").Inc()
	return operatorInfo, nil
}
