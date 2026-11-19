package model

import "slices"

// promptQueue is what the queue pill shows: the last fetched server queue
// plus prompts submitted behind a busy turn that no fetch has reported yet.
// Equal-text prompts match one to one, so a pending prompt that repeats one
// already queued may briefly under-count until the next fetch.
type promptQueue struct {
	fetched []string
	pending []pendingPrompt
	nextID  uint64
}

// pendingPrompt is a submitted prompt the server queue may not hold yet.
type pendingPrompt struct {
	id      uint64
	prompt  string
	settled bool
}

// Items returns the fetched queue followed by the unreported pending prompts.
func (q *promptQueue) Items() []string {
	if len(q.pending) == 0 {
		return q.fetched
	}
	items := slices.Clone(q.fetched)
	for _, entry := range q.pending {
		items = append(items, entry.prompt)
	}
	return items
}

// Shown returns the number of prompts the pill shows.
func (q *promptQueue) Shown() int {
	return len(q.fetched) + len(q.pending)
}

// SetFetched stores a fetched queue. A pending prompt the fetch reports is
// dropped, since the server now owns it. A settled prompt the fetch misses
// stays: the server queues a prompt only after it has built the run, which
// can be after the fetch that follows the submission.
func (q *promptQueue) SetFetched(fetched []string) {
	q.fetched = fetched
	unmatched := make(map[string]int, len(fetched))
	for _, prompt := range fetched {
		unmatched[prompt]++
	}
	q.pending = slices.DeleteFunc(q.pending, func(entry pendingPrompt) bool {
		if unmatched[entry.prompt] > 0 {
			unmatched[entry.prompt]--
			return true
		}
		return false
	})
}

// ClearServer empties the server side after the queue was cleared, along
// with settled submissions.
func (q *promptQueue) ClearServer() {
	q.fetched = nil
	q.DropSettled()
}

// DropSettled drops the settled submissions. Call it when the server
// reports the session idle, since an idle session has no queue and no
// prompt still on its way into one. Call it too when a run on the session
// fails, since the failed run's prompt may never reach the queue.
func (q *promptQueue) DropSettled() {
	q.pending = slices.DeleteFunc(q.pending, func(entry pendingPrompt) bool {
		return entry.settled
	})
}

// Add records a prompt being submitted behind a busy turn and returns its id.
func (q *promptQueue) Add(prompt string) uint64 {
	q.nextID++
	q.pending = append(q.pending, pendingPrompt{id: q.nextID, prompt: prompt})
	return q.nextID
}

// Settle marks a submission the server accepted. It keeps showing until a
// fetch reports it, its turn starts, the session goes idle or a run on the
// session fails.
func (q *promptQueue) Settle(id uint64) {
	for idx := range q.pending {
		if q.pending[idx].id == id {
			q.pending[idx].settled = true
		}
	}
}

// Drop removes a submission the server refused.
func (q *promptQueue) Drop(id uint64) {
	q.pending = slices.DeleteFunc(q.pending, func(entry pendingPrompt) bool {
		return entry.id == id
	})
}

// Started drops the oldest entry showing prompt, once its turn has taken
// it off the queue. Fetched entries are older than pending ones, so they
// are searched first. The fetched slice may have been handed out by Items,
// so it is replaced, not edited.
func (q *promptQueue) Started(prompt string) {
	if i := slices.Index(q.fetched, prompt); i >= 0 {
		q.fetched = slices.Delete(slices.Clone(q.fetched), i, i+1)
		return
	}
	if i := slices.IndexFunc(q.pending, func(entry pendingPrompt) bool {
		return entry.prompt == prompt
	}); i >= 0 {
		q.pending = slices.Delete(q.pending, i, i+1)
	}
}

// Remove drops the newest entry showing prompt, so the pill shrinks as
// soon as a pop lands rather than at the next fetch. Pending entries are
// newer than fetched ones, so they are searched first. The fetched slice
// may have been handed out by Items, so it is replaced, not edited.
func (q *promptQueue) Remove(prompt string) {
	for i := len(q.pending) - 1; i >= 0; i-- {
		if q.pending[i].prompt == prompt {
			q.pending = slices.Delete(q.pending, i, i+1)
			return
		}
	}
	for i := len(q.fetched) - 1; i >= 0; i-- {
		if q.fetched[i] == prompt {
			q.fetched = slices.Delete(slices.Clone(q.fetched), i, i+1)
			return
		}
	}
}

// Reset empties the queue, pending submissions included.
func (q *promptQueue) Reset() {
	q.fetched = nil
	q.pending = nil
}
