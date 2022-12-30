package api

import (
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/Rocket-Pool-Rescue-Node/rescue-api/services"
	"github.com/gorilla/mux"
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
	)

	sig, err := hex.DecodeString(strings.TrimPrefix(req.Sig, "0x"))
	if err != nil {
		return writeJSONError(w, err)
	}

	cred, err := ar.svc.CreateCredentialWithRetry([]byte(req.Msg), sig)
	if err != nil {
		return writeJSONError(w, err)
	}

	ar.logger.Info("Created credential",
		zap.String("nodeID", hex.EncodeToString(cred.Credential.NodeId)),
		zap.Int64("timestamp", cred.Credential.Timestamp))

	password, err := cred.Base64URLEncodePassword()
	if err != nil {
		return writeJSONError(w, err)
	}

	resp := CreateCredentialResponse{
		Username:  cred.Base64URLEncodeUsername(),
		Password:  password,
		Timestamp: cred.Credential.Timestamp,
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

func NewAPIRouter(path string, svc *services.Service, logger *zap.Logger) *mux.Router {
	// Create router.
	ah := &apiRouter{
		svc,
		logger,
	}
	r := mux.NewRouter()
	sr := r.PathPrefix(path).Subrouter()

	// Register handlers.
	sr.HandleFunc("/credentials", ah.wrapHandler(ah.CreateCredential)).Methods("POST")
	sr.HandleFunc("/credentials/", ah.wrapHandler(ah.CreateCredential)).Methods("POST")
	return r
}
