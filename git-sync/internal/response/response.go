// Package response writes the one document a caller consumes.
package response

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"git-suite/git-sync/internal/codes"
)

// Format selects the encoding of the stdout document.
type Format string

const (
	// JSON is the default encoding: one packed object and a trailing newline.
	JSON Format = "json"
	// BSON is one packed BSON document, with no trailing newline.
	BSON Format = "bson"
)

// Failure is the document written for every unsuccessful exit.
type Failure struct {
	Status  string `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Select chooses json or bson. An empty document value means the field was omitted.
// flagSet reports whether --format was passed. A bad name is an invalid invocation.
// When both are set they must agree; the error document uses the flag's encoding.
func Select(flagSet bool, flagValue, docValue string) (Format, error) {
	chosen := JSON
	if flagSet {
		parsed, err := parseName(flagValue)
		if err != nil {
			return JSON, err
		}
		chosen = parsed
	}
	if strings.TrimSpace(docValue) == "" {
		return chosen, nil
	}
	parsed, err := parseName(docValue)
	if err != nil {
		return chosen, err
	}
	if flagSet && parsed != chosen {
		return chosen, codes.New(codes.InvalidInvocation, "--format does not match the request format")
	}
	if !flagSet {
		return parsed, nil
	}
	return chosen, nil
}

func parseName(raw string) (Format, error) {
	switch strings.TrimSpace(raw) {
	case "json":
		return JSON, nil
	case "bson":
		return BSON, nil
	default:
		return JSON, codes.New(codes.InvalidInvocation, "format must be json or bson")
	}
}

// Write encodes one packed document. JSON ends with a newline. BSON does not.
func Write(w io.Writer, format Format, v any) {
	payload, err := encode(format, v)
	if err != nil {
		payload = fallback(format)
	}
	_, _ = w.Write(payload)
}

// WriteError maps err onto a failure document in the requested encoding.
func WriteError(w io.Writer, format Format, err error) {
	code, msg := codes.From(err)
	Write(w, format, Failure{Status: "failed", Code: code, Message: msg})
}

func encode(format Format, v any) ([]byte, error) {
	if format == BSON {
		return marshalBSON(v)
	}
	return marshalJSON(v)
}

func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func marshalBSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := bson.NewEncoder(bson.NewDocumentWriter(&buf))
	enc.UseJSONStructTags()
	enc.NilSliceAsEmpty()
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fallback(format Format) []byte {
	if format == BSON {
		raw, err := marshalBSON(Failure{Status: "failed", Code: codes.Internal, Message: "cannot write response"})
		if err == nil {
			return raw
		}
	}
	return []byte("{\"status\":\"failed\",\"code\":199,\"message\":\"cannot write response\"}\n")
}
