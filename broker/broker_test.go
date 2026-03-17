package broker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestBroker(t *testing.T) Broker {
	t.Helper()
	tmp := t.TempDir()
	b, err := New(Config{
		RequestsDir: filepath.Join(tmp, "requests"),
		HandoffDir:  filepath.Join(tmp, "handoff"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		b.Stop()
		b.Wait()
	})
	return b
}

func TestBrokerCreateRequest(t *testing.T) {
	tmp := t.TempDir()
	b, err := New(Config{
		RequestsDir: filepath.Join(tmp, "requests"),
		HandoffDir:  filepath.Join(tmp, "handoff"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { b.Stop(); b.Wait() }()

	id, err := b.CreateRequest("builder", "question", "How should I structure the API?")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if id != "000001" {
		t.Fatalf("expected ID 000001, got %s", id)
	}

	// Verify file on disk
	path := filepath.Join(tmp, "requests", "000001-request-builder.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read request file: %v", err)
	}
	if !strings.Contains(string(data), "How should I structure the API?") {
		t.Fatalf("request file missing content: %s", data)
	}
}

func TestBrokerSendResponse(t *testing.T) {
	tmp := t.TempDir()
	b, err := New(Config{
		RequestsDir: filepath.Join(tmp, "requests"),
		HandoffDir:  filepath.Join(tmp, "handoff"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { b.Stop(); b.Wait() }()

	id, err := b.CreateRequest("builder", "question", "What about X?")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	err = b.SendResponse(id, "Do it this way.")
	if err != nil {
		t.Fatalf("SendResponse: %v", err)
	}

	// Verify response file on disk
	path := filepath.Join(tmp, "requests", "000001-response-builder.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read response file: %v", err)
	}
	if !strings.Contains(string(data), "Do it this way.") {
		t.Fatalf("response file missing content: %s", data)
	}
}

func TestBrokerPushNewRequest(t *testing.T) {
	b := newTestBroker(t)

	ch, err := b.Subscribe("product-owner")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	_, err = b.CreateRequest("builder", "question", "Should I refactor?")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.Event != "new_request" {
			t.Fatalf("expected event new_request, got %s", evt.Event)
		}
		if evt.Role != "builder" {
			t.Fatalf("expected role builder, got %s", evt.Role)
		}
		if evt.Type != "question" {
			t.Fatalf("expected type question, got %s", evt.Type)
		}
		if evt.RequestID != "000001" {
			t.Fatalf("expected request_id 000001, got %s", evt.RequestID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBrokerPushResponseReady(t *testing.T) {
	b := newTestBroker(t)

	ch, err := b.Subscribe("builder")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	id, err := b.CreateRequest("builder", "question", "What about Y?")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	err = b.SendResponse(id, "Here is the answer.")
	if err != nil {
		t.Fatalf("SendResponse: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.Event != "response_ready" {
			t.Fatalf("expected event response_ready, got %s", evt.Event)
		}
		if evt.RequestID != "000001" {
			t.Fatalf("expected request_id 000001, got %s", evt.RequestID)
		}
		if evt.Content != "Here is the answer." {
			t.Fatalf("expected content 'Here is the answer.', got %q", evt.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBrokerWriteDoneRefused(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.CreateRequest("builder", "question", "Something")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	err = b.WriteDone("builder", "pass", "all good")
	if err == nil {
		t.Fatal("expected error for outstanding requests, got nil")
	}
	if !strings.Contains(err.Error(), "outstanding") {
		t.Fatalf("expected error containing 'outstanding', got %q", err.Error())
	}
}

func TestBrokerWriteDoneSuccess(t *testing.T) {
	tmp := t.TempDir()
	b, err := New(Config{
		RequestsDir: filepath.Join(tmp, "requests"),
		HandoffDir:  filepath.Join(tmp, "handoff"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { b.Stop(); b.Wait() }()

	id, err := b.CreateRequest("builder", "question", "Something")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	err = b.SendResponse(id, "Answer")
	if err != nil {
		t.Fatalf("SendResponse: %v", err)
	}

	err = b.WriteDone("builder", "pass", "all good")
	if err != nil {
		t.Fatalf("WriteDone: %v", err)
	}

	// Verify done file on disk
	path := filepath.Join(tmp, "handoff", "builder_done")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read done file: %v", err)
	}
	if !strings.Contains(string(data), "pass") {
		t.Fatalf("done file missing status: %s", data)
	}
}

func TestBrokerAmendments(t *testing.T) {
	tmp := t.TempDir()
	b, err := New(Config{
		RequestsDir: filepath.Join(tmp, "requests"),
		HandoffDir:  filepath.Join(tmp, "handoff"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { b.Stop(); b.Wait() }()

	id, err := b.CreateAmendment("api-spec", "Add a new endpoint for /health")
	if err != nil {
		t.Fatalf("CreateAmendment: %v", err)
	}
	if id != "001" {
		t.Fatalf("expected amendment ID 001, got %s", id)
	}

	amendments, err := b.ListAmendments()
	if err != nil {
		t.Fatalf("ListAmendments: %v", err)
	}
	if len(amendments) != 1 {
		t.Fatalf("expected 1 amendment, got %d", len(amendments))
	}
	if amendments[0].Spec != "api-spec" {
		t.Fatalf("expected spec api-spec, got %s", amendments[0].Spec)
	}

	// Verify file on disk
	pattern := filepath.Join(tmp, "requests", "amendments", "001-api-spec.md")
	data, err := os.ReadFile(pattern)
	if err != nil {
		t.Fatalf("read amendment file: %v", err)
	}
	if !strings.Contains(string(data), "Add a new endpoint for /health") {
		t.Fatalf("amendment file missing content: %s", data)
	}
}

func TestBrokerSequentialIDs(t *testing.T) {
	b := newTestBroker(t)

	id1, err := b.CreateRequest("builder", "question", "First")
	if err != nil {
		t.Fatalf("CreateRequest 1: %v", err)
	}
	id2, err := b.CreateRequest("builder", "question", "Second")
	if err != nil {
		t.Fatalf("CreateRequest 2: %v", err)
	}
	id3, err := b.CreateRequest("code-reviewer", "concern", "Third")
	if err != nil {
		t.Fatalf("CreateRequest 3: %v", err)
	}

	if id1 != "000001" {
		t.Fatalf("expected 000001, got %s", id1)
	}
	if id2 != "000002" {
		t.Fatalf("expected 000002, got %s", id2)
	}
	if id3 != "000003" {
		t.Fatalf("expected 000003, got %s", id3)
	}
}

func TestBrokerCheckResponse(t *testing.T) {
	b := newTestBroker(t)

	id, err := b.CreateRequest("builder", "question", "Check this")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	// Before response
	content, found, err := b.CheckResponse("builder", id)
	if err != nil {
		t.Fatalf("CheckResponse: %v", err)
	}
	if found {
		t.Fatal("expected not found before response")
	}
	if content != "" {
		t.Fatalf("expected empty content, got %q", content)
	}

	// Send response
	err = b.SendResponse(id, "Here you go")
	if err != nil {
		t.Fatalf("SendResponse: %v", err)
	}

	// After response
	content, found, err = b.CheckResponse("builder", id)
	if err != nil {
		t.Fatalf("CheckResponse: %v", err)
	}
	if !found {
		t.Fatal("expected found after response")
	}
	if content != "Here you go" {
		t.Fatalf("expected 'Here you go', got %q", content)
	}
}
