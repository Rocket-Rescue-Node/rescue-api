package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Rocket-Rescue-Node/credentials"
	"github.com/Rocket-Rescue-Node/rescue-api/api"
	"github.com/Rocket-Rescue-Node/rescue-api/database"
	"github.com/Rocket-Rescue-Node/rescue-api/models"
	"github.com/Rocket-Rescue-Node/rescue-api/services"
	"github.com/Rocket-Rescue-Node/rescue-api/tasks"
	"github.com/Rocket-Rescue-Node/rescue-proxy/metrics"
	"github.com/jonboulle/clockwork"

	"go.uber.org/zap"
)

func waitForTermination() {
	// Trap termination signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Block until a signal is received.
	<-c

	// Allow subsequent termination signals to quickly shut down by removing the trap.
	signal.Reset()
	close(c)
}

var logger *zap.Logger

// Logger initialization.
func initLogger(debug bool) error {
	var cfg zap.Config
	var err error

	if debug {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}

	logger, err = cfg.Build()
	return err
}

func main() {
	var cfg config
	var err error

	// Parse command line arguments.
	if cfg, err = parseArguments(); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing command-line arguments: %v\n", err)
		os.Exit(1)
	}

	// Initialize the metrics subsystem.
	metricsHandler, err := metrics.Init("rescue_api")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing metrics handler: %v\n", err)
		os.Exit(1)
	}

	// Initialize the logger.
	if err := initLogger(cfg.Debug); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		os.Exit(1)
	}

	// Connect to the database and initialize the database schema, if necessary.
	var db *sql.DB
	db, err = database.Open(cfg.DBPath)
	if err != nil {
		logger.Fatal("Unable to open the database connection", zap.Error(err))
	}
	defer db.Close()

	// Initialize the Credential Manager. This is used to create and verify credentials.
	cm := credentials.NewCredentialManager(sha256.New, []byte(cfg.CredentialSecret))

	// Background task to update the list of current Rocket Pool nodes.
	nodes := models.NewNodeRegistry()
	updateNodes := tasks.NewUpdateNodesTask(
		cfg.RescueProxyAPIAddr,
		cfg.RocketscanAPIURL,
		nodes,
		cfg.SecureGRPC,
		logger,
	)
	go updateNodes.Run()

	// Background task to update the list of withdrawal addresses.
	withdrawalAddresses := models.NewNodeRegistry()
	updateWithdrawalAddresses := tasks.NewUpdateWithdrawalAddressesTask(
		cfg.RescueProxyAPIAddr,
		withdrawalAddresses,
		cfg.SecureGRPC,
		logger,
	)
	go updateWithdrawalAddresses.Run()

	// Clock
	clock := clockwork.NewRealClock()

	// Services contain the business logic and are used by the API handlers.
	// Only CreateCredential is implemented for now.
	svcCfg := &services.ServiceConfig{
		DB:                   db,
		CM:                   cm,
		Nodes:                nodes,
		WithdrawalAddresses:  withdrawalAddresses,
		Logger:               logger,
		Clock:                clock,
		EnableSoloValidators: cfg.EnableSoloValidators,
	}
	svc := services.NewService(svcCfg)
	if err := svc.Init(); err != nil {
		logger.Fatal("Unable to initialize the service layer", zap.Error(err))
	}

	// Create the API router.
	path := "/rescue/v1/"
	router := api.NewAPIRouter(path, svc, cfg.AllowedOrigins, logger)

	// Listen on the provided address. This listener will be used by the HTTP server.
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to listen on provided address %s\n%v\n", cfg.ListenAddr, err)
		os.Exit(1)
	}

	// Listen on the metrics address.
	metricsListener, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to listen on provided metrics address %s\n%v\n", cfg.MetricsAddr, err)
		os.Exit(1)
	}

	// Spin up the HTTP server on a different goroutine, since it blocks.
	server := http.Server{
		Handler: router,
	}
	metricsServer := http.Server{
		Handler: metricsHandler,
	}
	var serverWaitGroup sync.WaitGroup
	serverWaitGroup.Add(2)
	go func() {
		logger.Info("Starting HTTP server", zap.String("url", cfg.ListenAddr))
		if err := server.Serve(listener); err != nil {
			logger.Error("HTTP server stopped", zap.Error(err))
		}
		serverWaitGroup.Done()
	}()
	go func() {
		logger.Info("Starting metrics HTTP server", zap.String("url", cfg.MetricsAddr))
		if err := metricsServer.Serve(metricsListener); err != nil {
			logger.Error("metrics HTTP server stopped", zap.Error(err))
		}
		serverWaitGroup.Done()
	}()

	waitForTermination()

	// Shut down gracefully
	logger.Info("Received termination signal, shutting down...")
	_ = server.Shutdown(context.Background())
	listener.Close()
	_ = metricsServer.Shutdown(context.Background())
	metricsListener.Close()

	// Wait for the listener/server to exit
	serverWaitGroup.Wait()

	// Shut down the service layer
	svc.Deinit()

	// Stop the background tasks
	if err = updateNodes.Stop(); err != nil {
		logger.Error("Error stopping node background tasks", zap.Error(err))
	}
	if err = updateWithdrawalAddresses.Stop(); err != nil {
		logger.Error("Error stopping withdrawal addresses background tasks", zap.Error(err))
	}

	metrics.Deinit()
	logger.Info("Shutdown complete")

	_ = logger.Sync()
}
