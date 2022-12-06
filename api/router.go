package api

import (
	"encoding/base64"
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

func (ar *apiRouter) CreateCredential(w http.ResponseWriter, r *http.Request) {
	// Try to decode the request body.
	var req CreateCredentialRequest
	if err := readJSONRequest(w, r, &req); err != nil {
		writeJSONError(w, err)
		return
	}

	ar.logger.Info("Got credential request",
		zap.String("address", req.Address),
		zap.String("msg", req.Msg),
		zap.String("sig", req.Sig),
		zap.String("version", req.Version),
	)

	sig, err := hex.DecodeString(strings.TrimPrefix(req.Sig, "0x"))
	if err != nil {
		writeJSONError(w, err)
		return
	}

	cred, err := ar.svc.CreateCredentialWithRetry([]byte(req.Msg), sig)
	if err != nil {
		writeJSONError(w, err)
		return
	}

	ar.logger.Info("Created credential",
		zap.String("nodeID", hex.EncodeToString(cred.Credential.NodeId)),
		zap.Int64("timestamp", cred.Credential.Timestamp))

	resp := CreateCredentialResponse{
		Username:  base64.URLEncoding.EncodeToString(cred.Credential.NodeId),
		Password:  base64.URLEncoding.EncodeToString(cred.Mac),
		Timestamp: cred.Credential.Timestamp,
	}

	writeJSONResponse(w, http.StatusCreated, resp, "")
}

func NewAPIRouter(path string, svc *services.Service, logger *zap.Logger) *mux.Router {
	ah := &apiRouter{
		svc,
		logger,
	}
	r := mux.NewRouter()
	sr := r.PathPrefix(path).Subrouter()
	sr.HandleFunc("/credentials", ah.CreateCredential).Methods("POST")
	sr.HandleFunc("/credentials/", ah.CreateCredential).Methods("POST")
	return r
}
