package api

import (
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/Rocket-Rescue-Node/rescue-api/services"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"go.uber.org/zap"
)

type apiRouter struct {
	svc    *services.Service
	logger *zap.Logger
}

func (ar *apiRouter) CreateCredential(w http.ResponseWriter, r *http.Request) error {
	// Try to decode the request body.
	var req CreateCredentialRequest
	if err := readJSONRequest(w, r, &req); err != nil {
		return writeJSONError(w, err)
	}

	ar.logger.Info("Got credential request",
		zap.String("address", req.Address),
		zap.String("msg", req.Msg),
		zap.String("sig", req.Sig),
		zap.String("version", req.Version),
		zap.Int("operator_type", int(req.operatorType)),
	)

	sig, err := hex.DecodeString(strings.TrimPrefix(req.Sig, "0x"))
	if err != nil {
		msg := "invalid signature"
		return writeJSONError(w, &decodingError{status: http.StatusBadRequest, msg: msg})
	}

	cred, err := ar.svc.CreateCredentialWithRetry([]byte(req.Msg), sig, req.operatorType)
	if err != nil {
		return writeJSONError(w, err)
	}

	ar.logger.Info("Created credential",
		zap.String("nodeID", hex.EncodeToString(cred.Credential.NodeId)),
		zap.Int("operator_type", int(cred.Credential.OperatorType)),
		zap.Int64("timestamp", cred.Credential.Timestamp))

	password, err := cred.Base64URLEncodePassword()
	if err != nil {
		return writeJSONError(w, err)
	}

	expires := time.Unix(cred.Credential.Timestamp, 0).Add(services.AuthValidityWindow(cred.Credential.OperatorType))

	resp := CreateCredentialResponse{
		Username:  cred.Base64URLEncodeUsername(),
		Password:  password,
		Timestamp: cred.Credential.Timestamp,
		ExpiresAt: expires.Unix(),
	}

	return writeJSONResponse(w, http.StatusCreated, resp, "")
}

func (ar *apiRouter) GetOperatorInfo(w http.ResponseWriter, r *http.Request) error {
	// Try to decode the request body.
	var req OperatorInfoRequest
	if err := readJSONRequest(w, r, &req); err != nil {
		return writeJSONError(w, err)
	}

	ar.logger.Info("Got operator info request",
		zap.String("address", req.Address),
		zap.String("msg", req.Msg),
		zap.String("sig", req.Sig),
		zap.String("version", req.Version),
		zap.Int("operator_type", int(req.operatorType)),
	)

	sig, err := hex.DecodeString(strings.TrimPrefix(req.Sig, "0x"))
	if err != nil {
		msg := "invalid signature"
		return writeJSONError(w, &decodingError{status: http.StatusBadRequest, msg: msg})
	}

	operatorInfo, err := ar.svc.GetOperatorInfo([]byte(req.Msg), sig, req.operatorType)
	if err != nil {
		return writeJSONError(w, err)
	}

	ar.logger.Info("Retrieved operator info",
		zap.String("nodeID", hex.EncodeToString(operatorInfo.NodeID.Bytes())),
		zap.Int("operator_type", int(operatorInfo.OperatorType)),
		zap.Int64("timestamp", operatorInfo.Timestamp))

	resp := OperatorInfoResponse{
		Timestamp:        operatorInfo.Timestamp,
		CredentialEvents: operatorInfo.CredentialEvents,
	}

	return writeJSONResponse(w, http.StatusCreated, resp, "")
}

// Wrapper to log unhandled errors.
// Note that this wrapper is only for last resort errors. For example, caused by
// error handling functions not being able to write a response to the client.
// TODO: ignore certain classes of errors, such as broken pipe,
// connection reset by peer, etc.
func (ar *apiRouter) wrapHandler(h func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			ar.logger.Error("Error handling request", zap.Error(err))
		}
	}
}

func NewAPIRouter(path string, svc *services.Service, origins []string, logger *zap.Logger) *mux.Router {
	// Create router.
	ah := &apiRouter{
		svc,
		logger,
	}
	r := mux.NewRouter()
	sr := r.PathPrefix(path).Subrouter()

	// Register handlers.
	allowedMethods := []string{"GET", "POST", "OPTIONS"}
	sr.HandleFunc("/credentials", ah.wrapHandler(ah.CreateCredential)).Methods(allowedMethods...)
	sr.HandleFunc("/credentials/", ah.wrapHandler(ah.CreateCredential)).Methods(allowedMethods...)
	sr.HandleFunc("/info", ah.wrapHandler(ah.GetOperatorInfo)).Methods(allowedMethods...)
	sr.HandleFunc("/info/", ah.wrapHandler(ah.GetOperatorInfo)).Methods(allowedMethods...)

	// CORS support.
	ch := cors.New(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   allowedMethods,
		ExposedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: false,
		Debug:            logger.Level() == zap.DebugLevel,
	})
	sr.Use(ch.Handler)

	return r
}
