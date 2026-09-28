package agent

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func typeOfString() reflect.Type { return reflect.TypeOf("") }

func TestIsMalformedChunkError(t *testing.T) {
	typeErr := &json.UnmarshalTypeError{Field: "content", Value: "number", Type: typeOfString()}
	if !isMalformedChunkError(typeErr) {
		t.Fatal("expected UnmarshalTypeError to be treated as malformed")
	}

	// The exact error surfaced by the upstream eino-ext acl/openai client.
	wrapped := errors.New(`json: cannot unmarshal number into Go struct field .content of type string`)
	if !isMalformedChunkError(wrapped) {
		t.Fatal("expected cannot-unmarshal ... of type error to be treated as malformed")
	}

	// Transport/API failures must NOT be swallowed.
	if isMalformedChunkError(errors.New("stream interrupted: context canceled")) {
		t.Fatal("context cancellation must not be treated as malformed")
	}
	if isMalformedChunkError(nil) {
		t.Fatal("nil must not be malformed")
	}
}