package api

import (
	"encoding/hex"
	"net/http"
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

func (ar *apiRouter) readJSONRequest(r *http.Request) (*CreateCredentialRequest, error) {
	out := new(CreateCredentialRequest)

	// Validate the request body
	if err := validateJSONRequest(r, out); err != nil {
		return nil, err
	}

	ar.logger.Info("Got valid request",
		zap.String("endpoint", r.URL.Path),
		zap.String("address", out.Address.Hex()),
		zap.String("msg", string(out.Msg)),
		zap.String("sig", hex.EncodeToString(out.Sig)),
		zap.String("version", out.Version),
		zap.Int("operator_type", int(out.operatorType)),
	)

	return out, nil
}

func (ar *apiRouter) CreateCredential(w http.ResponseWriter, r *http.Request) error {
	// Try to read the request
	req, err := ar.readJSONRequest(r)
	if err != nil {
		return writeJSONError(w, err)
	}

	// Create the credential
	cred, err := ar.svc.CreateCredentialWithRetry(req.Msg, req.Sig, req.Address, req.operatorType)
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
	// Try to read the request
	credReq, err := ar.readJSONRequest(r)
	if err != nil {
		return writeJSONError(w, err)
	}

	req := (*OperatorInfoRequest)(credReq)

	// Get operator info
	operatorInfo, err := ar.svc.GetOperatorInfo(req.Msg, req.Sig, req.Address, req.operatorType)
	if err != nil {
		return writeJSONError(w, err)
	}

	// Cred events retrieved
	ar.logger.Info("Retrieved operator info",
		zap.String("nodeID", req.Address.Hex()),
		zap.Int("operator_type", int(req.operatorType)),
	)

	// Get operator quota settings
	quotaSettings, err := services.GetQuotaJSON(req.operatorType)
	if err != nil {
		return err
	}

	resp := OperatorInfoResponse{
		CredentialEvents: operatorInfo.CredentialEvents,
		QuotaSettings:    &quotaSettings,
	}

	return writeJSONResponse(w, http.StatusOK, resp, "")
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

func MaxBytesReaderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Limit the size of the request body to 2 KB
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		next.ServeHTTP(w, r)
	})
}

func NewAPIRouter(path string, svc *services.Service, origins []string, logger *zap.Logger) *mux.Router {
	// Create router.
	ah := &apiRouter{
		svc,
		logger,
	}
	r := mux.NewRouter()
	sr := r.PathPrefix(path).Subrouter()

	// Enforce request byte limits
	sr.Use(MaxBytesReaderMiddleware)

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
