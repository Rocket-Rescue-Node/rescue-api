package services

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/Rocket-Rescue-Node/credentials"
	"github.com/Rocket-Rescue-Node/credentials/pb"
	"github.com/Rocket-Rescue-Node/rescue-api/models"
	authz "github.com/Rocket-Rescue-Node/rescue-api/models/authorization"
	"github.com/Rocket-Rescue-Node/rescue-api/util"

	"github.com/mattn/go-sqlite3"

	"go.uber.org/zap"
)

const (
	// A credential will be reused if it is valid for at least this long.
	credsMinValidityWindow = time.Duration(48) * time.Hour

	// The pattern for credential request messages.
	credentialRequestPattern = `(?i)^Rescue Node ([0-9]{10})$`
	// The maximum age for a credential request to be considered valid.
	credsRequestMaxAge = time.Duration(15) * time.Minute
)

type quota struct {
	// Max number of credentials that can be requested in a given time window.
	count uint
	// Time window in which the credential quota is calculated.
	window time.Duration
	// Duration a credential is valid for
	authValidityWindow time.Duration
}

var (
	// The delay between retries when creating a credential.
	// Values are taken from SQLite's default busy handler.
	dbTryDelayMs = []int{1, 2, 5, 10, 15, 20, 25, 25, 25, 50, 50, 100}

	quotas = map[credentials.OperatorType]quota{
		pb.OperatorType_OT_ROCKETPOOL: quota{
			count:              4,
			window:             time.Duration(365*24) * time.Hour,
			authValidityWindow: time.Duration(15*24) * time.Hour,
		},
		pb.OperatorType_OT_SOLO: quota{
			count:              3,
			window:             time.Duration(365*24) * time.Hour,
			authValidityWindow: time.Duration(10*24) * time.Hour,
		},
	}
)

func credsQuotaWindow(ot credentials.OperatorType) time.Duration {
	quota, ok := quotas[ot]
	if !ok {
		// Default to a year
		return time.Duration(365*24) * time.Hour
	}

	return quota.window
}

func credsQuota(ot credentials.OperatorType) int64 {
	quota, ok := quotas[ot]
	if !ok {
		// Default to one
		return 1
	}

	return int64(quota.count)
}

func AuthValidityWindow(ot credentials.OperatorType) time.Duration {
	quota, ok := quotas[ot]
	if !ok {
		// Default to 10 days
		return time.Duration(10*24) * time.Hour
	}

	return quota.authValidityWindow
}

// Creates a new credential for a node. If a valid credential already exists, it will be returned instead.
// This method will retry if creating a credential fails.
func (s *Service) CreateCredentialWithRetry(msg []byte, sig []byte, ot credentials.OperatorType) (*models.AuthenticatedCredential, error) {
	var cred *models.AuthenticatedCredential
	var err error

	var try int
	s.m.Counter("create_credential_with_retry").Inc()
	for try = range dbTryDelayMs {
		// Try to create the credential.
		if cred, err = s.CreateCredential(msg, sig, ot); err == nil {
			break
		}

		// Check wether the error is recoverable.
		var sqliteErr sqlite3.Error
		// If the error is not a recoverable SQLite error, we can't recover.
		if !errors.As(err, &sqliteErr) {
			s.m.Counter("create_credential_unrecoverable_error").Inc()
			break
		}
		if sqliteErr.Code != sqlite3.ErrLocked &&
			sqliteErr.Code != sqlite3.ErrBusy &&
			sqliteErr.Code != sqlite3.ErrConstraint {
			break
		}

		// Retry after a delay.
		sleepFor := dbTryDelayMs[try]
		s.logger.Warn("Failed to issue credential. Retrying",
			zap.Int("try", try),
			zap.Int("retryMs", sleepFor),
			zap.Error(err),
		)
		s.clock.Sleep(time.Duration(time.Duration(sleepFor) * time.Millisecond))
	}

	if err != nil {
		s.logger.Warn("Failed to issue credential. Giving up.",
			zap.Int("tries", try),
			zap.Error(err))
	}

	return cred, err
}

// Creates a new credential for a node. If a valid credential exists, it will be returned instead.
// No retry logic is implemented, so it is up to the caller to retry if it does not succeed.
func (s *Service) CreateCredential(msg []byte, sig []byte, ot credentials.OperatorType) (*models.AuthenticatedCredential, error) {
	var err error

	nodeID, err := util.RecoverAddressFromSignature(msg, sig)
	if err != nil {
		msg := "failed to recover nodeID from signature"
		s.logger.Warn(msg, zap.Error(err))
		s.m.Counter("create_credential_failed_auth").Inc()
		return nil, &AuthenticationError{msg}
	}
	s.logger.Info("Recovered nodeID from signature", zap.String("nodeID", nodeID.Hex()))

	// Check if the request is fresh.
	tsSecs, err := s.getTimestampFromRequest(string(msg))
	if err != nil {
		s.m.Counter("create_credential_invalid_timestamp").Inc()
		return nil, &ValidationError{"invalid timestamp"}
	}
	ts := time.Unix(tsSecs, 0)
	if time.Since(ts) > credsRequestMaxAge {
		s.m.Counter("create_credential_timestamp_too_old").Inc()
		return nil, &AuthenticationError{"timestamp is too old"}
	}

	// Check if this node is part of Rocket Pool, or a valid 0x01 credential.
	switch ot {
	case pb.OperatorType_OT_ROCKETPOOL:
		if !s.isNodeRegistered(nodeID) {
			s.m.Counter("create_credential_node_not_registered").Inc()
			return nil, &AuthorizationError{"node is not registered"}
		}
	case pb.OperatorType_OT_SOLO:
		if !s.enableSoloValidators {
			s.m.Counter("create_credential_solo_traffic_shedding").Inc()
			return nil, &AuthorizationError{"solo validators are currently not permitted"}
		}
		if !s.isWithdrawalAddress(nodeID) {
			s.m.Counter("create_credential_solo_not_withdrawal_address").Inc()
			return nil, &AuthorizationError{"wallet is not a withdrawal address for any validator"}
		}
	}

	// Make sure that the node is not banned from using the service.
	if !s.isNodeAuthorized(nodeID, authz.CredentialService) {
		s.m.Counter("create_credential_user_banned").Inc()
		return nil, &AuthorizationError{"node is not authorized"}
	}

	// Start a transaction to ensure that parallel requests do not create duplicate credentials.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	// Fetch the last credential and the number of credentials issued for this node in the current
	// window. This is done to ensure that:
	// - If a valid credential still exists, reissue it instead of creating a new one.
	// - The node does not request more credentials than allowed.
	now := s.clock.Now()
	// The timestamp of the first event in the current window.
	currentWindowStart := now.Add(-credsQuotaWindow(ot)).Unix()
	gcs := tx.Stmt(s.getCredEventsStmt)
	defer gcs.Close()
	row := gcs.QueryRow(nodeID.Bytes(), currentWindowStart, models.CredentialIssued, ot)
	var credsCount, lastCredTimestamp int64
	if err = row.Scan(&lastCredTimestamp, &credsCount); err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Reissue the last credential if it's still valid, and
	//  * It expires in more than credsMinValidityWindow seconds, or
	//  * No more credentials can be issued in the current window.
	created := time.Unix(lastCredTimestamp, 0)
	expires := created.Add(AuthValidityWindow(ot))
	if expires.After(now) && (expires.Sub(now) > credsMinValidityWindow || credsCount == credsQuota(ot)) {
		s.m.Counter("create_credential_recycled").Inc()
		return s.cm.Create(created, nodeID.Bytes(), ot)
	}

	// Has the node reached its quota for the current window?
	if credsCount >= credsQuota(ot) {
		s.logger.Warn("Node has reached its quota for the current window",
			zap.String("nodeID", nodeID.Hex()),
			zap.Int64("credsCount", credsCount),
			zap.Int64("credsQuota", credsQuota(ot)),
			zap.Int64("currentWindowStart", currentWindowStart),
			zap.String("operatorType", ot.String()),
		)
		s.m.Counter("create_credential_quota_exceeded").Inc()
		return nil, &AuthorizationError{"node has requested too many credentials"}
	}

	// Store a "credential issued" event in the database.
	acs := tx.Stmt(s.addCredEventStmt)
	defer acs.Close()
	_, err = acs.Exec(nodeID.Bytes(), now.Unix(), models.CredentialIssued, ot)
	if err != nil {
		return nil, err
	}

	// Create the credential.
	cred, err := s.cm.Create(now, nodeID.Bytes(), ot)
	if err != nil {
		return nil, err
	}

	// Commit the transaction.
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.logger.Info(
		"Issued credential",
		zap.String("nodeID", hex.EncodeToString(cred.Credential.NodeId)),
		zap.String("operatorType", ot.String()),
		zap.Int64("timestamp", cred.Credential.Timestamp),
	)

	s.m.Counter("create_credential_created").Inc()
	return cred, nil
}

func (s *Service) getTimestampFromRequest(msg string) (int64, error) {
	matches := s.credRequestRegexp.FindStringSubmatch(msg)
	if len(matches) != 2 {
		return -1, &ValidationError{"invalid request format"}
	}
	return strconv.ParseInt(matches[1], 10, 64)
}
