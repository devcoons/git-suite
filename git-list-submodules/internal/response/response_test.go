package response

import (
	"bytes"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestJSONIsPacked(t *testing.T) {
	var buf bytes.Buffer
	Write(&buf, JSON, Failure{Status: "failed", Code: 102, Message: "path is required"})
	raw := buf.String()
	if strings.Contains(raw, ": ") || strings.Contains(raw, ", ") || strings.Count(raw, "\n") != 1 || !strings.HasSuffix(raw, "\n") {
		t.Fatalf("json = %q", raw)
	}
}

func TestBSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	Write(&buf, BSON, Failure{Status: "failed", Code: 102, Message: "a & b"})
	if bytes.Contains(buf.Bytes(), []byte("\n")) {
		t.Fatal("bson document contains a newline")
	}
	dec := bson.NewDecoder(bson.NewDocumentReader(bytes.NewReader(buf.Bytes())))
	dec.UseJSONStructTags()
	var got Failure
	if err := dec.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" || got.Code != 102 || got.Message != "a & b" {
		t.Fatalf("%+v", got)
	}
}

func TestWriteFallsBackWithoutPanicking(t *testing.T) {
	var buf bytes.Buffer
	Write(&buf, JSON, make(chan int))
	if !bytes.Contains(buf.Bytes(), []byte(`"code":199`)) || !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Fatalf("fallback = %s", buf.Bytes())
	}

	buf.Reset()
	WriteError(&buf, JSON, nil)
	if !bytes.Contains(buf.Bytes(), []byte(`"code":199`)) {
		t.Fatalf("nil error = %s", buf.Bytes())
	}
}

func TestSelect(t *testing.T) {
	format, err := Select(false, "", "")
	if err != nil || format != JSON {
		t.Fatalf("%s %v", format, err)
	}
	format, err = Select(false, "", "bson")
	if err != nil || format != BSON {
		t.Fatalf("%s %v", format, err)
	}
	format, err = Select(true, "bson", "bson")
	if err != nil || format != BSON {
		t.Fatalf("%s %v", format, err)
	}
	if _, err = Select(true, "yaml", ""); err == nil {
		t.Fatal("expected an error")
	}
	format, err = Select(true, "bson", "json")
	if err == nil || format != BSON {
		t.Fatalf("%s %v", format, err)
	}
}
