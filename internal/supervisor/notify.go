package supervisor

import "xanax/internal/session"

// notifyEvent describes a desktop notification to raise.
type notifyEvent struct {
	title string
	body  string
}

// notificationFor decides whether a status transition warrants a desktop
// notification and what it should say. change=false means stay quiet.
//
// Rules (SPEC.md §12): notify when a session starts waiting for input, and on
// completion/failure. Never on cancelled (the user did that), and never repeat
// while already waiting (prev == new).
func notificationFor(prev, next session.Status, title, detail string) (notifyEvent, bool) {
	if next == prev {
		return notifyEvent{}, false
	}
	name := title
	if name == "" {
		name = "session"
	}
	switch next {
	case session.StatusWaiting:
		body := detail
		if body == "" {
			body = "waiting for input"
		}
		return notifyEvent{title: "Needs input · " + name, body: body}, true
	case session.StatusCompleted:
		return notifyEvent{title: "Completed · " + name, body: "the session finished"}, true
	case session.StatusFailed:
		body := detail
		if body == "" {
			body = "the session failed"
		}
		return notifyEvent{title: "Failed · " + name, body: body}, true
	default:
		return notifyEvent{}, false
	}
}

// raiseNotification fires an event through the supervisor's notifier, honoring
// the enabled flag and suppressing while a client is attached (you already see
// it). It is safe to call with a nil hub (pre-start failures).
func (s *Supervisor) raiseNotification(prev, next session.Status, detail string) {
	if !s.opts.Notify || s.notifyFn == nil {
		return
	}
	if s.hub != nil && s.hub.clientCount() > 0 {
		return // attached: no need to ping
	}
	ev, ok := notificationFor(prev, next, s.opts.Session.Title, detail)
	if !ok {
		return
	}
	s.notifyFn(ev.title, ev.body)
	s.log.Debug("notified", "title", ev.title)
}
