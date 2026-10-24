package report

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// defaultCloseTimeout bounds one close: the recording's retries (about two
// minutes) plus the store writes.
const defaultCloseTimeout = 5 * time.Minute

// CloseStore is what Closer reads and writes. DBStore is the real one.
type CloseStore interface {
	// HasRecording reports whether the call with conversationID has its
	// recording stored.
	HasRecording(ctx context.Context, conversationID string) (bool, error)
	// SaveRecording stores the call's recording and its SHA-256, once: a
	// second save of the same call changes nothing.
	SaveRecording(ctx context.Context, conversationID, contentType string, audio []byte) error
}

// Closer finishes a report after a call: it downloads the call's recording
// from ElevenLabs and stores it. CallFinished is the call engine's
// post-call hook. Create it with a struct literal.
type Closer struct {
	Store CloseStore
	// Audio fetches recordings; nil (no ELEVENLABS_API_KEY) stores none.
	Audio   AudioFetcher
	Backoff []time.Duration // waits between tries of a recording; DefaultBackoff when nil
	Timeout time.Duration   // bounds one close; defaultCloseTimeout when zero

	mu       sync.Mutex
	inflight map[string]bool // conversation ids being closed
	wg       sync.WaitGroup
}

// CallFinished closes the report in the background after a finished call
// and returns at once: it runs inside the ElevenLabs webhook request. Only a
// call that connected (status done) has a recording; a failed one is left
// for the retry's webhook. A repeated webhook while the first is still being
// closed is ignored, and a later one finds the recording already stored.
func (c *Closer) CallFinished(ctx context.Context, incidentID, conversationID, status string) {
	if status != "done" || conversationID == "" || !c.claim(conversationID) {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer c.release(conversationID)
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = defaultCloseTimeout
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		c.Close(ctx, incidentID, conversationID)
	}()
}

// Wait blocks until every close started by CallFinished is done. Tests use it.
func (c *Closer) Wait() { c.wg.Wait() }

// Close stores the call's recording. It only logs failures: there is no
// caller to report to, and a report without audio still renders.
func (c *Closer) Close(ctx context.Context, incidentID, conversationID string) {
	if err := c.record(ctx, conversationID); err != nil {
		log.Printf("report: incident %s conversation %s: recording not stored: %v", incidentID, conversationID, err)
	}
}

var errNoFetcher = errors.New("no ELEVENLABS_API_KEY to fetch it with")

// record downloads and stores the recording unless it is already stored.
func (c *Closer) record(ctx context.Context, conversationID string) error {
	if has, err := c.Store.HasRecording(ctx, conversationID); err != nil || has {
		return err
	}
	if c.Audio == nil {
		return errNoFetcher
	}
	backoff := c.Backoff
	if backoff == nil {
		backoff = DefaultBackoff
	}
	audio, typ, err := fetchWithRetry(ctx, c.Audio, conversationID, backoff)
	if err != nil {
		return err
	}
	if err := c.Store.SaveRecording(ctx, conversationID, typ, audio); err != nil {
		return err
	}
	log.Printf("report: conversation %s: recording stored (%d bytes, %s)", conversationID, len(audio), typ)
	return nil
}

func (c *Closer) claim(conversationID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inflight == nil {
		c.inflight = map[string]bool{}
	}
	if c.inflight[conversationID] {
		return false
	}
	c.inflight[conversationID] = true
	return true
}

func (c *Closer) release(conversationID string) {
	c.mu.Lock()
	delete(c.inflight, conversationID)
	c.mu.Unlock()
}
