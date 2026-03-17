// Package broker provides an actor-based event broker for the product owner
// request/response/amendment protocol. All mutable state is owned by a single
// goroutine; callers interact through typed command channels.
package broker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/corruptmemory/flywheel/internal/chanutil"
)

// BrokerEvent is pushed to subscribers.
type BrokerEvent struct {
	Event     string `json:"event"`
	RequestID string `json:"request_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Type      string `json:"type,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Content   string `json:"content,omitempty"`
}

// RequestEntry tracks a single PO request.
type RequestEntry struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	Response string `json:"response,omitempty"`
}

// AmendmentEntry tracks a spec amendment.
type AmendmentEntry struct {
	ID      string `json:"id"`
	Spec    string `json:"spec"`
	Content string `json:"content"`
}

// Config for creating a Broker.
type Config struct {
	RequestsDir string
	AmendDir    string
	HandoffDir  string
	EventBuffer int
}

// Broker is the public interface for the request/response broker.
type Broker interface {
	// Pipeline agent operations
	CreateRequest(role, reqType, content string) (string, error)
	CheckResponse(role, requestID string) (string, bool, error)
	ListMyRequests(role string) ([]RequestEntry, error)
	WriteDone(role, status, details string) error

	// Product Owner operations
	ListPendingRequests() ([]RequestEntry, error)
	ReadRequest(requestID string) (string, error)
	SendResponse(requestID, content string) error
	CreateAmendment(spec, content string) (string, error)
	ListAmendments() ([]AmendmentEntry, error)

	// Subscription
	Subscribe(role string) (<-chan BrokerEvent, error)
	Unsubscribe(role string)

	// Lifecycle
	Stop()
	Wait()
}

// --- command tags ---

type commandTag int

const (
	cmdCreateRequest commandTag = iota
	cmdCheckResponse
	cmdListMyRequests
	cmdWriteDone
	cmdListPending
	cmdReadRequest
	cmdSendResponse
	cmdCreateAmendment
	cmdListAmendments
	cmdSubscribe
	cmdUnsubscribe
)

// --- command struct ---

type brokerCommand struct {
	tag       commandTag
	role      string
	reqType   string
	content   string
	requestID string
	status    string
	details   string
	spec      string
	result    chan any
}

func (c brokerCommand) WithResult(ch chan any) brokerCommand {
	c.result = ch
	return c
}

// --- unexported actor struct ---

type broker struct {
	commands chan brokerCommand
	done     chan struct{}
	wg       sync.WaitGroup
	cfg      Config
}

// New creates and starts a new Broker actor.
func New(cfg Config) (Broker, error) {
	if cfg.EventBuffer == 0 {
		cfg.EventBuffer = 16
	}
	if cfg.AmendDir == "" {
		cfg.AmendDir = filepath.Join(cfg.RequestsDir, "amendments")
	}
	if err := os.MkdirAll(cfg.RequestsDir, 0755); err != nil {
		return nil, fmt.Errorf("broker: mkdir requests: %w", err)
	}
	if err := os.MkdirAll(cfg.AmendDir, 0755); err != nil {
		return nil, fmt.Errorf("broker: mkdir amend: %w", err)
	}
	if err := os.MkdirAll(cfg.HandoffDir, 0755); err != nil {
		return nil, fmt.Errorf("broker: mkdir handoff: %w", err)
	}

	b := &broker{
		commands: make(chan brokerCommand, 64),
		done:     make(chan struct{}),
		cfg:      cfg,
	}
	b.wg.Add(1)
	go b.run()
	return b, nil
}

// --- public methods (channel senders) ---

func (b *broker) CreateRequest(role, reqType, content string) (string, error) {
	return chanutil.SendReceive[brokerCommand, string](b.commands, brokerCommand{
		tag:     cmdCreateRequest,
		role:    role,
		reqType: reqType,
		content: content,
	})
}

func (b *broker) CheckResponse(role, requestID string) (string, bool, error) {
	r, err := chanutil.SendReceive[brokerCommand, checkResult](b.commands, brokerCommand{
		tag:       cmdCheckResponse,
		role:      role,
		requestID: requestID,
	})
	return r.content, r.found, err
}

func (b *broker) ListMyRequests(role string) ([]RequestEntry, error) {
	return chanutil.SendReceive[brokerCommand, []RequestEntry](b.commands, brokerCommand{
		tag:  cmdListMyRequests,
		role: role,
	})
}

func (b *broker) WriteDone(role, status, details string) error {
	return chanutil.SendReceiveError(b.commands, brokerCommand{
		tag:     cmdWriteDone,
		role:    role,
		status:  status,
		details: details,
	})
}

func (b *broker) ListPendingRequests() ([]RequestEntry, error) {
	return chanutil.SendReceive[brokerCommand, []RequestEntry](b.commands, brokerCommand{
		tag: cmdListPending,
	})
}

func (b *broker) ReadRequest(requestID string) (string, error) {
	return chanutil.SendReceive[brokerCommand, string](b.commands, brokerCommand{
		tag:       cmdReadRequest,
		requestID: requestID,
	})
}

func (b *broker) SendResponse(requestID, content string) error {
	return chanutil.SendReceiveError(b.commands, brokerCommand{
		tag:       cmdSendResponse,
		requestID: requestID,
		content:   content,
	})
}

func (b *broker) CreateAmendment(spec, content string) (string, error) {
	return chanutil.SendReceive[brokerCommand, string](b.commands, brokerCommand{
		tag:     cmdCreateAmendment,
		spec:    spec,
		content: content,
	})
}

func (b *broker) ListAmendments() ([]AmendmentEntry, error) {
	return chanutil.SendReceive[brokerCommand, []AmendmentEntry](b.commands, brokerCommand{
		tag: cmdListAmendments,
	})
}

func (b *broker) Subscribe(role string) (<-chan BrokerEvent, error) {
	return chanutil.SendReceive[brokerCommand, <-chan BrokerEvent](b.commands, brokerCommand{
		tag:  cmdSubscribe,
		role: role,
	})
}

func (b *broker) Unsubscribe(role string) {
	_ = chanutil.SendReceiveError(b.commands, brokerCommand{
		tag:  cmdUnsubscribe,
		role: role,
	})
}

func (b *broker) Stop() {
	defer func() { recover() }()
	close(b.done)
}

func (b *broker) Wait() {
	b.wg.Wait()
}

// --- actor run loop ---

// checkResult is a value type returned from CheckResponse through the result channel.
type checkResult struct {
	content string
	found   bool
}

func (b *broker) run() {
	counter := 0
	amendCounter := 0
	requests := make(map[string]*RequestEntry)
	amendments := make([]AmendmentEntry, 0)
	subscribers := make(map[string]chan BrokerEvent)
	outstanding := make(map[string]map[string]bool)

	defer func() {
		for _, ch := range subscribers {
			close(ch)
		}
		b.wg.Done()
	}()

	push := func(role string, evt BrokerEvent) {
		if ch, ok := subscribers[role]; ok {
			select {
			case ch <- evt:
			default:
			}
		}
	}

	first100 := func(s string) string {
		if len(s) <= 100 {
			return s
		}
		return s[:100]
	}

	handleCreateRequest := func(msg brokerCommand) {
		counter++
		id := fmt.Sprintf("%06d", counter)
		entry := &RequestEntry{
			ID:      id,
			Role:    msg.role,
			Type:    msg.reqType,
			Content: msg.content,
		}
		requests[id] = entry

		if outstanding[msg.role] == nil {
			outstanding[msg.role] = make(map[string]bool)
		}
		outstanding[msg.role][id] = true

		// Write to disk
		filename := fmt.Sprintf("%s-request-%s.md", id, msg.role)
		body := fmt.Sprintf("# Request %s — %s — %s\n\n%s\n", id, msg.role, msg.reqType, msg.content)
		_ = os.WriteFile(filepath.Join(b.cfg.RequestsDir, filename), []byte(body), 0644)

		push("product-owner", BrokerEvent{
			Event:     "new_request",
			RequestID: id,
			Role:      msg.role,
			Type:      msg.reqType,
			Summary:   first100(msg.content),
		})

		msg.result <- id
	}

	handleCheckResponse := func(msg brokerCommand) {
		entry, ok := requests[msg.requestID]
		if !ok {
			msg.result <- checkResult{}
			return
		}
		if entry.Response == "" {
			msg.result <- checkResult{}
			return
		}
		msg.result <- checkResult{content: entry.Response, found: true}
	}

	handleListMyRequests := func(msg brokerCommand) {
		var result []RequestEntry
		for _, entry := range requests {
			if entry.Role == msg.role {
				result = append(result, *entry)
			}
		}
		msg.result <- result
	}

	handleWriteDone := func(msg brokerCommand) {
		if msg.role != "product-owner" {
			if set, ok := outstanding[msg.role]; ok && len(set) > 0 {
				msg.result <- fmt.Errorf("role %q has outstanding requests; cannot write done file", msg.role)
				return
			}
		}

		filename := msg.role + "_done"
		if msg.role == "product-owner" {
			filename = "po_done"
		}
		body := fmt.Sprintf("%s\n%s\n", msg.status, msg.details)
		err := os.WriteFile(filepath.Join(b.cfg.HandoffDir, filename), []byte(body), 0644)
		msg.result <- err
	}

	handleListPending := func(msg brokerCommand) {
		var result []RequestEntry
		for _, entry := range requests {
			if entry.Response == "" {
				result = append(result, *entry)
			}
		}
		msg.result <- result
	}

	handleReadRequest := func(msg brokerCommand) {
		entry, ok := requests[msg.requestID]
		if !ok {
			msg.result <- fmt.Errorf("request %s not found", msg.requestID)
			return
		}
		msg.result <- entry.Content
	}

	handleSendResponse := func(msg brokerCommand) {
		entry, ok := requests[msg.requestID]
		if !ok {
			msg.result <- fmt.Errorf("request %s not found", msg.requestID)
			return
		}
		entry.Response = msg.content

		if set, ok := outstanding[entry.Role]; ok {
			delete(set, msg.requestID)
		}

		// Write to disk
		filename := fmt.Sprintf("%s-response-%s.md", entry.ID, entry.Role)
		body := fmt.Sprintf("# Response %s\n\n%s\n", entry.ID, msg.content)
		_ = os.WriteFile(filepath.Join(b.cfg.RequestsDir, filename), []byte(body), 0644)

		push(entry.Role, BrokerEvent{
			Event:     "response_ready",
			RequestID: entry.ID,
			Content:   msg.content,
		})

		msg.result <- error(nil)
	}

	handleCreateAmendment := func(msg brokerCommand) {
		amendCounter++
		id := fmt.Sprintf("%03d", amendCounter)
		entry := AmendmentEntry{
			ID:      id,
			Spec:    msg.spec,
			Content: msg.content,
		}
		amendments = append(amendments, entry)

		// Write to disk
		sanitized := sanitizeSpec(msg.spec)
		filename := fmt.Sprintf("%s-%s.md", id, sanitized)
		body := fmt.Sprintf("# Amendment %s — %s\n\n%s\n", id, msg.spec, msg.content)
		_ = os.WriteFile(filepath.Join(b.cfg.AmendDir, filename), []byte(body), 0644)

		msg.result <- id
	}

	handleListAmendments := func(msg brokerCommand) {
		result := make([]AmendmentEntry, len(amendments))
		copy(result, amendments)
		msg.result <- result
	}

	handleSubscribe := func(msg brokerCommand) {
		ch := make(chan BrokerEvent, b.cfg.EventBuffer)
		subscribers[msg.role] = ch
		msg.result <- (<-chan BrokerEvent)(ch)
	}

	handleUnsubscribe := func(msg brokerCommand) {
		if ch, ok := subscribers[msg.role]; ok {
			close(ch)
			delete(subscribers, msg.role)
		}
		msg.result <- error(nil)
	}

	for {
		select {
		case <-b.done:
			return
		case msg := <-b.commands:
			switch msg.tag {
			case cmdCreateRequest:
				handleCreateRequest(msg)
			case cmdCheckResponse:
				handleCheckResponse(msg)
			case cmdListMyRequests:
				handleListMyRequests(msg)
			case cmdWriteDone:
				handleWriteDone(msg)
			case cmdListPending:
				handleListPending(msg)
			case cmdReadRequest:
				handleReadRequest(msg)
			case cmdSendResponse:
				handleSendResponse(msg)
			case cmdCreateAmendment:
				handleCreateAmendment(msg)
			case cmdListAmendments:
				handleListAmendments(msg)
			case cmdSubscribe:
				handleSubscribe(msg)
			case cmdUnsubscribe:
				handleUnsubscribe(msg)
			}
		}
	}
}

// sanitizeSpec makes a spec name safe for filenames.
func sanitizeSpec(spec string) string {
	var b strings.Builder
	for _, r := range spec {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.ToLower(b.String())
}
