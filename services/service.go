package services

import (
	"context"
	"database/sql"
	"regexp"
	"time"

	creds "github.com/Rocket-Pool-Rescue-Node/credentials"
	"github.com/Rocket-Pool-Rescue-Node/rescue-api/models"
	authz "github.com/Rocket-Pool-Rescue-Node/rescue-api/models/authorization"
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
	db                   *sql.DB
	getCredEventsStmt    *sql.Stmt
	addCredEventStmt     *sql.Stmt
	isNodeAuthorizedStmt *sql.Stmt

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
