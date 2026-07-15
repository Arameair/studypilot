package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
)

type errorEnvelope struct {
	Error struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		Recoverable bool   `json:"recoverable"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string, recoverable bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	value := errorEnvelope{}
	value.Error.Code, value.Error.Message, value.Error.Recoverable = code, message, recoverable
	_ = json.NewEncoder(w).Encode(value)
}

func writeApplicationError(w http.ResponseWriter, err error) {
	kind := application.Classify(err)
	status, code, message, recoverable := http.StatusInternalServerError, "internal", "StudyPilot could not complete the request.", false
	switch kind {
	case application.ErrorInvalidInput:
		status, code, message = http.StatusBadRequest, "invalid_input", "The request is invalid."
	case application.ErrorNotFound:
		status, code, message = http.StatusNotFound, "not_found", "The requested StudyPilot resource was not found."
	case application.ErrorConflict, application.ErrorCollision, application.ErrorAmbiguous:
		status, code, message, recoverable = http.StatusConflict, "conflict", "The requested operation conflicts with current state.", true
	case application.ErrorUnsafe:
		status, code, message = http.StatusForbidden, "unsafe", "The requested operation is not allowed."
	case application.ErrorCancelled:
		status, code, message, recoverable = http.StatusConflict, "cancelled", "The request was cancelled.", true
	case application.ErrorTimeout:
		status, code, message, recoverable = http.StatusGatewayTimeout, "timeout", "The operation timed out.", true
	case application.ErrorUncertain:
		status, code, message, recoverable = http.StatusInternalServerError, "recovery_required", "Persistence is uncertain; inspect current state before retrying.", true
	}
	writeError(w, status, code, message, recoverable)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_input", "Content-Type must be application/json.", false)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "The request body is too large.", false)
		} else {
			writeError(w, http.StatusBadRequest, "invalid_input", "The JSON request body is invalid.", false)
		}
		return false
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_input", "The JSON request body must contain one object.", false)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "The HTTP method is not allowed for this endpoint.", false)
	return false
}

func safeReference(value string) bool {
	if len(value) < 1 || len(value) > 128 || strings.HasPrefix(value, ".") {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' || character == '~' {
			continue
		}
		return false
	}
	return true
}

func safeModelReference(value string) bool {
	if len(value) < 1 || len(value) > 160 || strings.HasPrefix(value, "/") || strings.Contains(value, "..") || strings.Contains(value, `\`) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' || character == '/' {
			continue
		}
		return false
	}
	return true
}
