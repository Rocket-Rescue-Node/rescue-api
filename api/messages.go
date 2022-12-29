package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/Rocket-Pool-Rescue-Node/rescue-api/services"
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
	Address string `json:"address"`
	Msg     string `json:"msg"`
	Sig     string `json:"sig"`
	Version string `json:"version"`
}

type CreateCredentialResponse struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Timestamp int64  `json:"timestamp"`
}

func readJSONRequest(w http.ResponseWriter, r *http.Request, req interface{}) error {
	var err error

	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		const msg = "Content-Type is not application/json"
		return &decodingError{status: http.StatusUnsupportedMediaType, msg: msg}
	}

	// Limit the size of the request body to 2 KB
	r.Body = http.MaxBytesReader(w, r.Body, 2048)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err = dec.Decode(&req)
	if err != nil || dec.Decode(&struct{}{}) != io.EOF {
		const msg = "invalid or multiple JSON objects in request body"
		return &decodingError{status: http.StatusBadRequest, msg: msg}
	}

	return nil
}

func writeJSONResponse(w http.ResponseWriter, code int, data interface{}, err string) error {
	resp, _ := json.Marshal(response{Data: data, Error: err})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, e := w.Write(resp)
	return e
}

func writeJSONError(w http.ResponseWriter, err error) error {
	var de *decodingError
	if ok := errors.As(err, &de); ok {
		return writeJSONResponse(w, de.status, nil, de.msg)
	}

	if ok := errors.Is(err, &services.ValidationError{}); ok {
		return writeJSONResponse(w, http.StatusBadRequest, nil, err.Error())
	}

	if ok := errors.Is(err, &services.AuthenticationError{}); ok {
		return writeJSONResponse(w, http.StatusUnauthorized, nil, err.Error())
	}

	if ok := errors.Is(err, &services.AuthorizationError{}); ok {
		return writeJSONResponse(w, http.StatusForbidden, nil, err.Error())
	}

	return writeJSONResponse(w, http.StatusInternalServerError, nil, "internal server error")
}
