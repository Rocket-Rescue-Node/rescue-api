package services

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	creds "github.com/Rocket-Rescue-Node/credentials"
	"github.com/Rocket-Rescue-Node/credentials/pb"
	"github.com/Rocket-Rescue-Node/rescue-api/models"
	authz "github.com/Rocket-Rescue-Node/rescue-api/models/authorization"
	"github.com/Rocket-Rescue-Node/rescue-api/util"
	"github.com/Rocket-Rescue-Node/rescue-proxy/metrics"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
)

const (
	// The maximum age of the node registry before being considered outdated.
	nodeRegistryMaxAge = 1 * time.Hour
)

type ValidationError struct {
	msg string
}

func (v *ValidationError) Error() string {
	return v.msg
}

func (v *ValidationError) Is(err error) bool {
	_, ok := err.(*ValidationError)
	return ok
}

type AuthenticationError struct {
	msg string
}

func (a *AuthenticationError) Error() string {
	return a.msg
}

func (a *AuthenticationError) Is(err error) bool {
	_, ok := err.(*AuthenticationError)
	return ok
}

type AuthorizationError struct {
	msg string
}

func (a *AuthorizationError) Error() string {
	return a.msg
}

func (a *AuthorizationError) Is(err error) bool {
	_, ok := err.(*AuthorizationError)
	return ok
}

// ServiceConfig contains the configuration for a Service.
type ServiceConfig struct {
	DB                   *sql.DB
	CM                   *creds.CredentialManager
	Nodes                *models.NodeRegistry
	WithdrawalAddresses  *models.NodeRegistry
	Logger               *zap.Logger
	Clock                clockwork.Clock
	EnableSoloValidators bool
}

// Services contain business logic, are responsible for interacting with the database,
// and with external services.
// They are called by the API handlers.
type Service struct {
	// Credentials
	cm                *creds.CredentialManager
	credRequestRegexp *regexp.Regexp

	// Active nodes on the Rocket Pool network
	nodes *models.NodeRegistry

	// Active validators' withdrawal addresses
	withdrawalAddresses *models.NodeRegistry

	// Database
	db                         *sql.DB
	getCredEventsStmt          *sql.Stmt
	getCredEventTimestampsStmt *sql.Stmt
	addCredEventStmt           *sql.Stmt
	isNodeAuthorizedStmt       *sql.Stmt

	m      *metrics.MetricsRegistry
	logger *zap.Logger

	clock clockwork.Clock

	enableSoloValidators bool
}

func NewService(config *ServiceConfig) *Service {
	re := regexp.MustCompile(credentialRequestPattern)
	return &Service{
		cm:                   config.CM,
		db:                   config.DB,
		nodes:                config.Nodes,
		withdrawalAddresses:  config.WithdrawalAddresses,
		credRequestRegexp:    re,
		logger:               config.Logger,
		clock:                config.Clock,
		enableSoloValidators: config.EnableSoloValidators,
	}
}

func (s *Service) Init() error {
	s.m = metrics.NewMetricsRegistry("service")
	if err := s.createTables(); err != nil {
		return err
	}
	return s.prepareStatements()
}

func (s *Service) migrateTables() error {

	// If operator_type isn't on the table, create it
	var c int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info("credential_events") where name = "operator_type";`).Scan(&c)
	if err != nil {
		return err
	}

	if c == 0 {

		// Update the primary key by copying the table, dropping the old version, and renaming it.
		// Insert 0s for operator_type, as all events prior to the migration were RP NOs, who have operator_type 0.
		_, err := s.db.Exec(`
			CREATE TABLE _credential_events_copy (
				node_id BLOB(20) NOT NULL,
				timestamp INTEGER NOT NULL,
				type INTEGER CHECK (type >= 0 AND type <= 1) NOT NULL,
				operator_type INTEGER NOT NULL,
				PRIMARY KEY (node_id, operator_type, timestamp)
			);

			INSERT INTO _credential_events_copy (node_id, timestamp, type, operator_type)
				SELECT node_id, timestamp, type, 0 FROM credential_events;
			DROP TABLE credential_events;
			ALTER TABLE _credential_events_copy RENAME TO credential_events;
		`)

		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) createTables() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS credential_events (
			node_id BLOB(20) NOT NULL,
			timestamp INTEGER NOT NULL,
			type INTEGER CHECK (type >= 0 AND type <= 1) NOT NULL,
			PRIMARY KEY (node_id, timestamp)
		);
		CREATE TABLE IF NOT EXISTS authorization_rules (
			node_id BLOB(20) NOT NULL,
			resource INTEGER CHECK (resource >= 0 AND resource <=1) NOT NULL,
			action INTEGER CHECK (action >= 0 AND action <= 1) NOT NULL,
			PRIMARY KEY (node_id, resource)
		);
	`)
	if err != nil {
		return err
	}

	err = s.migrateTables()
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) prepareStatements() error {
	var err error

	if s.getCredEventsStmt, err = s.db.Prepare(`
		SELECT COALESCE(MAX(timestamp), 0), COUNT(*) FROM credential_events
		WHERE node_id = ? AND timestamp > ? AND type = ? AND operator_type = ?;
	`); err != nil {
		return err
	}

	if s.getCredEventTimestampsStmt, err = s.db.Prepare(`
		SELECT timestamp FROM credential_events WHERE node_id = ? AND timestamp > ? AND timestamp <= ? AND type = ? AND operator_type = ? ORDER BY timestamp DESC;
	`); err != nil {
		return err
	}

	if s.addCredEventStmt, err = s.db.Prepare(`
		INSERT INTO credential_events (node_id, timestamp, type, operator_type) VALUES (?, ?, ?, ?);
	`); err != nil {
		return err
	}

	if s.isNodeAuthorizedStmt, err = s.db.Prepare(`
		SELECT node_id FROM authorization_rules
		WHERE node_id = ? AND resource = ? AND action = ?
		LIMIT 1;
	`); err != nil {
		return err
	}

	return nil
}

// isNodeRegistered checks if a Node is registered on the Rocket Pool network.
func (s *Service) isNodeRegistered(nodeID *models.NodeID) bool {
	// If the node registry is stale, all nodes are considered unregistered.
	if s.clock.Now().After(s.nodes.LastUpdated.Add(nodeRegistryMaxAge)) {
		s.logger.Error("Node registry is too old, refusing access to node",
			zap.String("nodeID", nodeID.Hex()))
		s.m.Counter("old_node_registry").Inc()
		return false
	}
	return s.nodes.Has(*nodeID)
}

// isWithdrawalAddress checks if an address is the withdrawal credential for at least one active validator.
func (s *Service) isWithdrawalAddress(nodeID *models.NodeID) bool {
	// If the registry is stale, all nodes are considered invalid.
	if s.clock.Now().After(s.withdrawalAddresses.LastUpdated.Add(nodeRegistryMaxAge)) {
		s.logger.Error("Withdrawal Address registry is too old, refusing access to user",
			zap.String("withdrawal_address", nodeID.Hex()))
		s.m.Counter("old_withdrawal_address_registry").Inc()
		return false
	}
	return s.withdrawalAddresses.Has(*nodeID)
}

// isNodeAuthorized checks if a Node is authorized to access a Resource.
func (s *Service) isNodeAuthorized(nodeID *models.NodeID, svc authz.Resource) bool {
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelReadCommitted})
	if err != nil {
		s.logger.Error("Failed to begin database transaction", zap.Error(err))
		return false
	}
	defer rollback(tx)
	stmt := tx.Stmt(s.isNodeAuthorizedStmt)
	defer stmt.Close()
	rows, err := stmt.Query(nodeID.Bytes(), svc, authz.Deny)
	if err != nil {
		s.logger.Error("Failed to query database", zap.Error(err))
		return false
	}
	defer rows.Close()
	return !rows.Next()
}

func (s *Service) checkRequestAge(msg *[]byte) error {
	// Check if the request is fresh
	tsSecs, err := s.getTimestampFromRequest(string(*msg))
	if err != nil {
		s.m.Counter("invalid_timestamp").Inc()
		return &ValidationError{"invalid timestamp"}
	}
	ts := time.Unix(tsSecs, 0)
	if time.Since(ts) > credsRequestMaxAge {
		s.m.Counter("timestamp_too_old").Inc()
		return &AuthenticationError{"timestamp is too old"}
	}

	return nil
}

func (s *Service) getNodeID(msg *[]byte, sig *[]byte) (*common.Address, error) {
	// Recover the nodeID
	nodeID, err := util.RecoverAddressFromSignature(*msg, *sig)
	if err != nil {
		msg := "failed to recover nodeID from signature"
		s.logger.Warn(msg, zap.Error(err))
		s.m.Counter("failed_auth").Inc()
		return nil, &AuthenticationError{msg}
	}
	s.logger.Info("Recovered nodeID from signature", zap.String("nodeID", nodeID.Hex()))
	return nodeID, nil
}

func (s *Service) checkNodeAuthorization(nodeID *models.NodeID, ot creds.OperatorType) error {
	// Check if this node is part of Rocket Pool, or a valid 0x01 credential
	switch ot {
	case pb.OperatorType_OT_ROCKETPOOL:
		if !s.isNodeRegistered(nodeID) {
			s.m.Counter("node_not_registered").Inc()
			return &AuthorizationError{"node is not registered"}
		}
	case pb.OperatorType_OT_SOLO:
		if !s.enableSoloValidators {
			s.m.Counter("solo_traffic_shedding").Inc()
			return &AuthorizationError{"solo validators are currently not permitted"}
		}
		if !s.isWithdrawalAddress(nodeID) {
			s.m.Counter("solo_not_withdrawal_address").Inc()
			return &AuthorizationError{"wallet is not a withdrawal address for any validator"}
		}
	}

	// Make sure that the node is not banned from using the service.
	if !s.isNodeAuthorized(nodeID, authz.CredentialService) {
		s.m.Counter("user_banned").Inc()
		return &AuthorizationError{"node is not authorized"}
	}

	return nil
}

func (s *Service) validateSignedRequest(msg *[]byte, sig *[]byte, expectedNodeId common.Address, ot pb.OperatorType) (*common.Address, error) {
	// Check request age
	if err := s.checkRequestAge(msg); err != nil {
		return nil, err
	}

	// Recover nodeID
	nodeID, err := s.getNodeID(msg, sig)
	if err != nil {
		return nil, err
	}

	// Check if the nodeID matches the expected nodeID
	if *nodeID != expectedNodeId {
		return nil, &AuthenticationError{fmt.Sprintf("provided node id (%s) did not match address (%s) which signed the message", expectedNodeId.Hex(), nodeID.Hex())}
	}

	// Check node authz
	if err := s.checkNodeAuthorization(nodeID, ot); err != nil {
		return nil, err
	}

	return nodeID, nil
}

func (s *Service) Deinit() {
	// Close prepared statements
	for _, stmt := range []**sql.Stmt{
		&s.getCredEventsStmt,
		&s.addCredEventStmt,
		&s.isNodeAuthorizedStmt,
	} {
		if *stmt == nil {
			continue
		}
		(*stmt).Close()
		*stmt = nil
	}
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
