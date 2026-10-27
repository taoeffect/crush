package permission

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

// hookApprovalKey is the unexported context key used to mark a tool call as
// pre-approved by a PreToolUse hook. The value is the tool call ID so an
// approval can't be reused across calls that happen to share a context.
type hookApprovalKey struct{}

// WithHookApproval returns a context that marks the given tool call ID as
// pre-approved by a hook. When the permission service sees a matching
// request it short-circuits the normal prompt and grants immediately.
func WithHookApproval(ctx context.Context, toolCallID string) context.Context {
	return context.WithValue(ctx, hookApprovalKey{}, toolCallID)
}

// hookApproved reports whether the context carries a hook approval for the
// given tool call ID.
func hookApproved(ctx context.Context, toolCallID string) bool {
	if toolCallID == "" {
		return false
	}
	v, _ := ctx.Value(hookApprovalKey{}).(string)
	return v == toolCallID
}

type CreatePermissionRequest struct {
	SessionID   string `json:"session_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
}

type PermissionNotification struct {
	SessionID  string `json:"session_id"`
	ToolCallID string `json:"tool_call_id"`
	Granted    bool   `json:"granted"`
	Denied     bool   `json:"denied"`
	// Cancelled is set when the request was abandoned before a decision
	// (the turn was cancelled while the prompt was open).
	Cancelled bool `json:"cancelled"`
}

// Outcome is how a permission prompt the user saw was resolved.
type Outcome string

const (
	OutcomeGranted   Outcome = "granted"
	OutcomeDenied    Outcome = "denied"
	OutcomeCancelled Outcome = "cancelled"
)

// PromptObserver hears about every prompt the user is shown and how it
// was resolved, in that order. Auto-approved calls show no prompt and
// are not reported. Calls run off the requesting goroutine, so a slow
// observer never delays the prompt or the tool.
type PromptObserver interface {
	PromptShown(ctx context.Context, req PermissionRequest)
	PromptResolved(ctx context.Context, req PermissionRequest, outcome Outcome)
}

type PermissionRequest struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
}

type Service interface {
	pubsub.Subscriber[PermissionRequest]
	// GrantPersistent grants a permission request and remembers the grant
	// for the session. It returns true if this call actually resolved the
	// pending request; false if the request had already been resolved
	// (e.g., by another concurrent caller) or is unknown.
	GrantPersistent(permission PermissionRequest) bool
	// Grant grants a permission request. It returns true if this call
	// actually resolved the pending request; false if the request had
	// already been resolved or is unknown.
	Grant(permission PermissionRequest) bool
	// Deny denies a permission request. It returns true if this call
	// actually resolved the pending request; false if the request had
	// already been resolved or is unknown.
	Deny(permission PermissionRequest) bool
	Request(ctx context.Context, opts CreatePermissionRequest) (bool, error)
	// PendingRequests returns every request still awaiting a decision,
	// in the order they were published. Prompts are not serialized, so
	// several can be open at once. A request is only reported while it
	// can still be resolved; a request whose waiter was cancelled is not
	// returned.
	PendingRequests() []PermissionRequest
	// AutoApproveSession adds an auto-approval hold on a session, so
	// every permission request in it is granted without prompting.
	// Holds are counted: a session stays auto-approved until every
	// holder has revoked.
	AutoApproveSession(sessionID string)
	// RevokeAutoApproveSession drops one auto-approval hold on a
	// session. Revoking a session that has no hold is a no-op.
	RevokeAutoApproveSession(sessionID string)
	SetSkipRequests(skip bool)
	SkipRequests() bool
	SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification]
}

// PermissionKey is a composite key for session permission lookups.
type PermissionKey struct {
	SessionID string
	ToolName  string
	Action    string
	Path      string
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	notificationBroker *pubsub.Broker[PermissionNotification]
	workingDir         string
	sessionPermissions *csync.Map[PermissionKey, bool]
	pendingRequests    *csync.Map[string, chan bool]
	// autoApproveSessions counts the outstanding auto-approval holds
	// per session ID. It is a count rather than a flag so overlapping
	// holders — two `crush run --continue` invocations resolve to the
	// same session — cannot revoke each other's approval and hang the
	// run that is still going.
	autoApproveSessions   map[string]int
	autoApproveSessionsMu sync.RWMutex
	skip                  atomic.Bool
	allowedTools          []string
	observer              PromptObserver

	// openRequests holds the prompts published and not yet returned
	// from Request, in publication order.
	openRequests   []PermissionRequest
	openRequestsMu sync.Mutex
}

// resolve atomically removes the pending request entry for the given
// permission and, if it was still pending, publishes exactly one
// PermissionNotification and forwards the outcome to the waiter on
// respCh. It returns true if this call resolved the request, false if
// it had already been resolved (e.g., by another concurrent caller) or
// the request ID is unknown. cancelled marks a request that was
// abandoned without a decision; granted and denied are then false.
//
// If onResolve is non-nil it runs after the pending entry has been
// taken but before the notification is published or the waiter is
// unblocked. This lets GrantPersistent record the session permission
// only when it actually wins the race, so a losing GrantPersistent
// that lost to a Deny does not leak an auto-approve entry.
//
// All three public resolution methods (Grant, GrantPersistent, Deny)
// route through this helper, as does Request's own cancellation path,
// so multi-subscriber UIs can race safely: the first caller wins, the
// rest become no-ops.
func (s *permissionService) resolve(permission PermissionRequest, granted, denied, cancelled bool, onResolve func()) bool {
	respCh, ok := s.pendingRequests.Take(permission.ID)
	if !ok {
		return false
	}

	if onResolve != nil {
		onResolve()
	}

	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		SessionID:  permission.SessionID,
		ToolCallID: permission.ToolCallID,
		Granted:    granted,
		Denied:     denied,
		Cancelled:  cancelled,
	})

	// respCh is buffered (cap 1) and only ever has at most one sender
	// per request because Take removes the entry under the map lock,
	// so this send never blocks.
	respCh <- granted

	return true
}

func (s *permissionService) GrantPersistent(permission PermissionRequest) bool {
	// Record the persistent grant only if this call wins the
	// pending-request race. Otherwise a losing GrantPersistent that
	// lost to a Deny would still leave an auto-approve entry behind,
	// silently flipping later denied calls to allowed.
	return s.resolve(permission, true, false, false, func() {
		s.sessionPermissions.Set(PermissionKey{
			SessionID: permission.SessionID,
			ToolName:  permission.ToolName,
			Action:    permission.Action,
			Path:      permission.Path,
		}, true)
	})
}

func (s *permissionService) Grant(permission PermissionRequest) bool {
	return s.resolve(permission, true, false, false, nil)
}

func (s *permissionService) Deny(permission PermissionRequest) bool {
	return s.resolve(permission, false, true, false, nil)
}

func (s *permissionService) Request(ctx context.Context, opts CreatePermissionRequest) (bool, error) {
	if s.skip.Load() {
		return true, nil
	}

	// Check if the tool/action combination is in the allowlist
	commandKey := opts.ToolName + ":" + opts.Action
	if slices.Contains(s.allowedTools, commandKey) || slices.Contains(s.allowedTools, opts.ToolName) {
		return true, nil
	}

	// A PreToolUse hook that returned decision=allow stamps the context
	// with the tool call ID. Treat that as a pre-approval and skip the
	// prompt entirely. We still publish a granted notification so the UI
	// and audit subscribers see the outcome.
	if hookApproved(ctx, opts.ToolCallID) {
		s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
			SessionID:  opts.SessionID,
			ToolCallID: opts.ToolCallID,
			Granted:    true,
		})
		return true, nil
	}

	// Tell the UI a permission was requested. This happens before any
	// of the checks below so that every request is visible the moment it
	// is made, including one that ends up waiting on a human.
	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		SessionID:  opts.SessionID,
		ToolCallID: opts.ToolCallID,
	})

	s.autoApproveSessionsMu.RLock()
	autoApprove := s.autoApproveSessions[opts.SessionID] > 0
	s.autoApproveSessionsMu.RUnlock()

	if autoApprove {
		s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
			SessionID:  opts.SessionID,
			ToolCallID: opts.ToolCallID,
			Granted:    true,
		})
		return true, nil
	}

	fileInfo, err := os.Stat(opts.Path)
	dir := opts.Path
	if err == nil {
		if fileInfo.IsDir() {
			dir = opts.Path
		} else {
			dir = filepath.Dir(opts.Path)
		}
	}

	if dir == "." {
		dir = s.workingDir
	}
	permission := PermissionRequest{
		ID:          uuid.New().String(),
		Path:        dir,
		SessionID:   opts.SessionID,
		ToolCallID:  opts.ToolCallID,
		ToolName:    opts.ToolName,
		Description: opts.Description,
		Action:      opts.Action,
		Params:      opts.Params,
	}

	if _, ok := s.sessionPermissions.Get(PermissionKey{
		SessionID: permission.SessionID,
		ToolName:  permission.ToolName,
		Action:    permission.Action,
		Path:      permission.Path,
	}); ok {
		s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
			SessionID:  opts.SessionID,
			ToolCallID: opts.ToolCallID,
			Granted:    true,
		})
		return true, nil
	}

	// Nothing is serialized from here on. Requests are registered by ID
	// and answered by ID, so each one waits on its own channel: a
	// request nobody answers blocks its own tool call and nothing else.
	// This used to run under a workspace-wide mutex held across the
	// human wait, which froze every other permission request in the
	// workspace — including those of unrelated sessions — behind the
	// unanswered one.
	respCh := make(chan bool, 1)
	s.pendingRequests.Set(permission.ID, respCh)
	// Backstop only: both select arms below take the entry themselves,
	// so this is a no-op unless a future exit path is added above them.
	defer s.pendingRequests.Del(permission.ID)

	s.openRequestsMu.Lock()
	s.openRequests = append(s.openRequests, permission)
	s.openRequestsMu.Unlock()
	defer s.forgetOpenRequest(permission.ID)

	// Publish the request
	s.Publish(pubsub.CreatedEvent, permission)
	resolved := s.observe(ctx, permission)

	select {
	case <-ctx.Done():
		// A cancelled run still owes its subscribers a cancelled terminal
		// event. Without one, a client that queued this request keeps a
		// dead entry — and possibly an open dialog — that nothing can
		// resolve, so later live requests wait behind it and every
		// answer to it is a no-op. resolve's Take is the single-winner
		// rule: if a concurrent Grant or Deny already took the entry,
		// this publishes nothing.
		if s.resolve(permission, false, false, true, nil) {
			resolved(OutcomeCancelled)
		} else {
			// A decision won the race; resolve always delivers it.
			resolved(outcomeOf(<-respCh))
		}
		return false, ctx.Err()
	case granted := <-respCh:
		resolved(outcomeOf(granted))
		return granted, nil
	}
}

// observe reports the prompt to the observer and returns the function
// that reports its outcome. Both run on one goroutine so the observer
// always hears them in order, and outlive ctx so a cancelled turn still
// reports the cancellation.
func (s *permissionService) observe(ctx context.Context, req PermissionRequest) func(Outcome) {
	if s.observer == nil {
		return func(Outcome) {}
	}
	ctx = context.WithoutCancel(ctx)
	outcome := make(chan Outcome, 1)
	go func() {
		s.observer.PromptShown(ctx, req)
		s.observer.PromptResolved(ctx, req, <-outcome)
	}()
	return func(o Outcome) { outcome <- o }
}

func outcomeOf(granted bool) Outcome {
	if granted {
		return OutcomeGranted
	}
	return OutcomeDenied
}

// PendingRequests returns the open requests that Grant/Deny can still
// resolve. A request stays in openRequests until Request returns, which
// is after its pendingRequests entry is taken, so the entry is checked
// as well.
func (s *permissionService) PendingRequests() []PermissionRequest {
	s.openRequestsMu.Lock()
	defer s.openRequestsMu.Unlock()
	pending := make([]PermissionRequest, 0, len(s.openRequests))
	for _, req := range s.openRequests {
		if _, ok := s.pendingRequests.Get(req.ID); ok {
			pending = append(pending, req)
		}
	}
	return pending
}

func (s *permissionService) forgetOpenRequest(id string) {
	s.openRequestsMu.Lock()
	defer s.openRequestsMu.Unlock()
	s.openRequests = slices.DeleteFunc(s.openRequests, func(req PermissionRequest) bool {
		return req.ID == id
	})
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.autoApproveSessionsMu.Lock()
	s.autoApproveSessions[sessionID]++
	s.autoApproveSessionsMu.Unlock()
}

func (s *permissionService) RevokeAutoApproveSession(sessionID string) {
	s.autoApproveSessionsMu.Lock()
	if holds := s.autoApproveSessions[sessionID]; holds > 1 {
		s.autoApproveSessions[sessionID] = holds - 1
	} else {
		delete(s.autoApproveSessions, sessionID)
	}
	s.autoApproveSessionsMu.Unlock()
}

func (s *permissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification] {
	return s.notificationBroker.Subscribe(ctx)
}

func (s *permissionService) SetSkipRequests(skip bool) {
	s.skip.Store(skip)
}

func (s *permissionService) SkipRequests() bool {
	return s.skip.Load()
}

// NewPermissionService creates the permission service. observer may be
// nil.
func NewPermissionService(workingDir string, skip bool, allowedTools []string, observer PromptObserver) Service {
	svc := &permissionService{
		Broker:              pubsub.NewBroker[PermissionRequest](),
		notificationBroker:  pubsub.NewBroker[PermissionNotification](),
		workingDir:          workingDir,
		sessionPermissions:  csync.NewMap[PermissionKey, bool](),
		autoApproveSessions: make(map[string]int),
		allowedTools:        allowedTools,
		pendingRequests:     csync.NewMap[string, chan bool](),
		observer:            observer,
	}
	svc.skip.Store(skip)
	return svc
}
