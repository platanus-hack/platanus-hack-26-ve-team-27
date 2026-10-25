package report

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/trace"
)

// Defaults for the Closer fields left zero.
const (
	// defaultCloseTimeout bounds one close: the recording's retries (about
	// two minutes), the wait for the verdict and the store writes.
	defaultCloseTimeout = 5 * time.Minute
	// defaultVerdictWait is how long the seal waits, after the recording,
	// for the call's verdict and the family message (#21 stores them).
	defaultVerdictWait = 60 * time.Second
	defaultPollEvery   = time.Second
	// defaultRecoverAfter is how long after a call ended a page view may
	// restart a close that never finished (a restart killed it).
	defaultRecoverAfter = 5 * time.Minute
)

// CloseStore is what Closer reads and writes. DBStore is the real one.
type CloseStore interface {
	// HasRecording reports whether the call with conversationID has its
	// recording stored.
	HasRecording(ctx context.Context, conversationID string) (bool, error)
	// SaveRecording stores the call's recording and its SHA-256, once: a
	// second save of the same call changes nothing.
	SaveRecording(ctx context.Context, conversationID, contentType string, audio []byte) error
	// Load reads the incident's record in one snapshot.
	Load(ctx context.Context, incidentID string) (Record, error)
	// SaveSeal stores the incident's seal unless it has one, and reports
	// whether this one was stored.
	SaveSeal(ctx context.Context, incidentID string, s Seal) (bool, error)
}

// Closer finishes a report after a call: it downloads the call's recording
// from ElevenLabs and stores it, waits a bounded time for the verdict and
// the family message, then seals the report with the server's Ed25519 key
// (docs/adr/0005-incident-report-seal.md). CallFinished is the call
// engine's post-call hook. Create it with a struct literal.
type Closer struct {
	Store CloseStore
	// Audio fetches recordings; nil (no ELEVENLABS_API_KEY) stores none.
	Audio AudioFetcher
	// Signer is the server's Ed25519 key (EMERGENCY_ED25519_SEED); nil
	// leaves every report unsealed.
	Signer       ed25519.PrivateKey
	Backoff      []time.Duration  // waits between tries of a recording; DefaultBackoff when nil
	Timeout      time.Duration    // bounds one close; defaultCloseTimeout when zero
	VerdictWait  time.Duration    // defaultVerdictWait when zero
	PollEvery    time.Duration    // defaultPollEvery when zero
	RecoverAfter time.Duration    // defaultRecoverAfter when zero
	Now          func() time.Time // time.Now when nil

	mu        sync.Mutex
	inflight  map[string]bool // conversation ids being closed
	recovered map[string]bool // conversation ids Recover already restarted
	wg        sync.WaitGroup
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

// Close stores the call's recording, then seals the report, unless a call
// of the incident is still under way or a newer one finished (see seal).
// It only logs failures: there is no caller to report to, and a report
// without audio or without a seal still renders. A recording that never
// arrives does not stop the seal: the sealed document then has no
// recording hash.
func (c *Closer) Close(ctx context.Context, incidentID, conversationID string) {
	if err := c.record(ctx, incidentID, conversationID); err != nil {
		log.Printf("report: incident %s conversation %s: recording not stored: %v", incidentID, conversationID, err)
		trace.Log(incidentID, "recording_stored", "ok", false, "conversation_id", conversationID, "error", err)
	}
	if c.Signer == nil {
		return
	}
	c.awaitVerdict(ctx, incidentID, conversationID)
	if _, err := c.seal(ctx, incidentID, conversationID); err != nil {
		log.Printf("report: incident %s: not sealed: %v", incidentID, err)
		trace.Log(incidentID, "sealed", "ok", false, "error", err)
	}
}

// awaitVerdict polls until the call has its verdict and the incident its
// family message (or a no-duress verdict that needs none), or VerdictWait
// passes, so the seal covers them when they come in time.
func (c *Closer) awaitVerdict(ctx context.Context, incidentID, conversationID string) {
	wait, every := c.VerdictWait, c.PollEvery
	if wait <= 0 {
		wait = defaultVerdictWait
	}
	if every <= 0 {
		every = defaultPollEvery
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		rec, err := c.Store.Load(ctx, incidentID)
		if err == nil && (rec.Seal != nil || verdictAndNotice(rec, conversationID)) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			log.Printf("report: incident %s: sealing without waiting longer for the verdict or the family message", incidentID)
			return
		case <-tick.C:
		}
	}
}

func verdictAndNotice(rec Record, conversationID string) bool {
	for _, cl := range rec.Calls {
		if cl.ConversationID == conversationID {
			return cl.Verdict != "" && (rec.Incident.NotifiedAt != nil || familyNotNeeded(rec))
		}
	}
	return false
}

// Seal signs and stores the incident's report as it is now, once. It
// reports whether this call sealed it; a report already sealed, of an
// incident without a call, or with a call still ringing or in progress, is
// left as it is.
func (c *Closer) Seal(ctx context.Context, incidentID string) (bool, error) {
	return c.seal(ctx, incidentID, "")
}

// seal is Seal on behalf of the call with conversationID, "" for none: it
// also leaves the report alone unless that call is the incident's newest
// finished one. ElevenLabs can send a "done" webhook for a phone that rang
// out, and that call stays done while the engine places the retry
// (call.Engine.await). Sealing for it would leave the retry's transcript
// and recording out of the seal for good, since nothing reseals: the
// retry's own close seals instead, or Recover with the newest finished call
// once the retry failed too. Every check reads the snapshot the document is
// built from.
func (c *Closer) seal(ctx context.Context, incidentID, conversationID string) (bool, error) {
	if c.Signer == nil {
		return false, errors.New("no signing key")
	}
	rec, err := c.Store.Load(ctx, incidentID)
	if err != nil {
		return false, err
	}
	if rec.Seal != nil || rec.Incident.Branch != contract.BranchCall {
		return false, nil
	}
	if c.ongoing(rec) {
		log.Printf("report: incident %s: not sealed yet: a call is still ringing or in progress", incidentID)
		return false, nil
	}
	if newest, ok := newestDone(rec); conversationID != "" && (!ok || newest.ConversationID != conversationID) {
		log.Printf("report: incident %s: not sealed for conversation %s: it is not the newest finished call", incidentID, conversationID)
		return false, nil
	}
	// Postgres keeps microseconds: the stored sealed_at equals the document's.
	now := c.now().UTC().Truncate(time.Microsecond)
	pub := c.Signer.Public().(ed25519.PublicKey)
	doc, err := Document(rec, now, pub)
	if err != nil {
		return false, fmt.Errorf("document: %w", err)
	}
	stored, err := c.Store.SaveSeal(ctx, incidentID, Seal{SealedAt: now, Document: doc, Signature: Sign(doc, c.Signer), PublicKey: pub})
	if err != nil {
		return false, err
	}
	if stored {
		log.Printf("report: incident %s: sealed (%d bytes, key %s)", incidentID, len(doc), Fingerprint(pub)[:19])
		sum := sha256.Sum256(doc)
		trace.Log(incidentID, "sealed", "ok", true, "bytes", len(doc), "sha256", hex.EncodeToString(sum[:])[:12], "key", Fingerprint(pub)[:19], "calls", len(rec.Calls))
	}
	return stored, nil
}

// Recover restarts the close of a report that a restart left unfinished,
// or whose retry failed after the first call's close declined to seal: on
// a view of a call report with no call under way that is not sealed (or
// whose newest finished call has no recording), once that call ended
// RecoverAfter ago. It runs at most once per call per process, in the
// background.
func (c *Closer) Recover(rec Record) {
	if c == nil || rec.Incident.Branch != contract.BranchCall {
		return
	}
	// Nothing seals while a retry rings or runs, and restarting now would
	// spend the finished call's one recovery: a retry that connects seals
	// on its own webhook, and one that fails leaves it to a later view.
	if c.ongoing(rec) {
		return
	}
	cl, ok := newestDone(rec)
	if !ok {
		return
	}
	// Never add a recording after the seal: the sealed document would not
	// cover it.
	needsSeal := c.Signer != nil && rec.Seal == nil
	needsAudio := c.Audio != nil && cl.Recording == nil && rec.Seal == nil
	ended := cl.RequestedAt
	if cl.EndedAt != nil {
		ended = *cl.EndedAt
	}
	after := c.RecoverAfter
	if after <= 0 {
		after = defaultRecoverAfter
	}
	if (needsSeal || needsAudio) && c.now().Sub(ended) >= after && c.firstRecovery(cl.ConversationID) {
		log.Printf("report: incident %s conversation %s: restarting its close", rec.Incident.ID, cl.ConversationID)
		c.CallFinished(context.Background(), rec.Incident.ID, cl.ConversationID, cl.Status)
	}
}

// stuckAfter is how long a call may stay requested or in progress before
// the seal stops waiting for it. A real "Mamá" call ends well within it; a
// row past it was lost (the server died between placing the call and its
// webhook, or no webhook ever came), and must not keep the report unsealed
// for good.
const stuckAfter = 15 * time.Minute

// ongoing reports whether a call of the incident is still requested,
// ringing or in progress, requested less than stuckAfter ago: neither its
// webhook nor its failure came yet.
func (c *Closer) ongoing(rec Record) bool {
	for _, cl := range rec.Calls {
		if (cl.Status == "requested" || cl.Status == "in_progress") && c.now().Sub(cl.RequestedAt) < stuckAfter {
			return true
		}
	}
	return false
}

// newestDone is the incident's newest call that connected (status done)
// with a conversation id: the one whose close seals the report.
func newestDone(rec Record) (Call, bool) {
	for i := len(rec.Calls) - 1; i >= 0; i-- {
		if cl := rec.Calls[i]; cl.Status == "done" && cl.ConversationID != "" {
			return cl, true
		}
	}
	return Call{}, false
}

func (c *Closer) firstRecovery(conversationID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.recovered == nil {
		c.recovered = map[string]bool{}
	}
	if c.recovered[conversationID] {
		return false
	}
	c.recovered[conversationID] = true
	return true
}

func (c *Closer) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

var errNoFetcher = errors.New("no ELEVENLABS_API_KEY to fetch it with")

// record downloads and stores the recording unless it is already stored.
func (c *Closer) record(ctx context.Context, incidentID, conversationID string) error {
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
	sum := sha256.Sum256(audio)
	trace.Log(incidentID, "recording_stored", "ok", true, "conversation_id", conversationID, "bytes", len(audio), "sha256", hex.EncodeToString(sum[:])[:12], "type", typ)
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
