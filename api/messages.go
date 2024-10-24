package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/Rocket-Rescue-Node/credentials"
	"github.com/Rocket-Rescue-Node/credentials/pb"
	"github.com/Rocket-Rescue-Node/rescue-api/services"
	"github.com/ethereum/go-ethereum/common"
)

type response struct {
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

type decodingError struct {
	status int
	msg    string
}

func (br *decodingError) Error() string {
	return br.msg
}

type CreateCredentialRequest struct {
	Address common.Address `json:"address"`
	Msg     []byte         `json:"msg"`
	Sig     []byte         `json:"sig"`
	Version string         `json:"version"`

	operatorType credentials.OperatorType `json:"-"`
}

func (c *CreateCredentialRequest) UnmarshalJSON(data []byte) error {
	type Alias CreateCredentialRequest
	aux := &struct {
		Address string `json:"address"`
		Msg     string `json:"msg"`
		Sig     string `json:"sig"`

		// Populates the `Version` field
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Convert Address
	c.Address = common.HexToAddress(aux.Address)

	// Convert Msg
	c.Msg = []byte(aux.Msg)

	// Convert Sig
	c.Sig = []byte(aux.Sig)

	return nil
}

type CreateCredentialResponse struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Timestamp int64  `json:"timestamp"`
	ExpiresAt int64  `json:"expiresAt"`
}

type OperatorInfoRequest CreateCredentialRequest

type OperatorInfoResponse struct {
	CredentialEvents []int64          `json:"credentialEvents"`
	QuotaSettings    *json.RawMessage `json:"quotaSettings"`
}

func validateJSONRequest(r *http.Request, req interface{}) error {
	var err error

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		const msg = "Content-Type is not application/json"
		return &decodingError{status: http.StatusUnsupportedMediaType, msg: msg}
	}

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err = dec.Decode(&req)
	if err != nil || dec.Decode(&struct{}{}) != io.EOF {
		const msg = "invalid or multiple JSON objects in request body"
		return &decodingError{status: http.StatusBadRequest, msg: msg}
	}

	// Check querystring for operator_type
	operatorType, ok := r.URL.Query()["operator_type"]
	if ok && len(operatorType) > 0 && strings.EqualFold(operatorType[0], "solo") {
		req.(*CreateCredentialRequest).operatorType = credentials.OperatorType(pb.OperatorType_OT_SOLO)
	}

	return nil
}

func writeJSONResponse(w http.ResponseWriter, code int, data interface{}, err string) error {
	resp, merr := json.Marshal(response{Data: data, Error: err})
	if merr != nil {
		return merr
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, e := w.Write(resp)
	return e
}

func writeJSONError(w http.ResponseWriter, err error) error {
	var de *decodingError
	switch {
	case errors.As(err, &de):
		return writeJSONResponse(w, de.status, nil, de.msg)
	case errors.Is(err, &services.ValidationError{}):
		return writeJSONResponse(w, http.StatusBadRequest, nil, err.Error())
	case errors.Is(err, &services.AuthenticationError{}):
		return writeJSONResponse(w, http.StatusUnauthorized, nil, err.Error())
	case errors.Is(err, &services.AuthorizationError{}):
		return writeJSONResponse(w, http.StatusForbidden, nil, err.Error())
	default:
		return writeJSONResponse(w, http.StatusInternalServerError, nil, "internal server error")
	}
}
