// Command setvoice gives a device its own "Mamá" voice from an audio file,
// straight against the database and ElevenLabs: the demo's fallback when the
// app cannot upload it (#107). It does what POST /devices/{id}/voice does,
// without the signature or the rate limit:
//
//	go run ./cmd/setvoice --device <device_id> --file mama.mp3
//	go run ./cmd/setvoice --device <device_id> --delete
//
// Running it states that the voice's owner agreed to the clone: it records
// voice_consent_at and the audio's SHA-256, never the audio. It reads
// DATABASE_URL and ELEVENLABS_API_KEY (the key needs the Voices write
// permission). The database must already have migration 0008 (the server
// applies it on boot).
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/db"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/voice"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, voice.BaseURL))
}

// run is main without the process: tests pass their own env, output and
// ElevenLabs URL. It returns the exit code.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer, elevenLabsURL string) int {
	fs := flag.NewFlagSet("setvoice", flag.ContinueOnError)
	fs.SetOutput(stderr)
	device := fs.String("device", "", "device_id (required)")
	file := fs.String("file", "", "audio file: mp3, wav, m4a, ogg or webm, up to 10 MB")
	del := fs.Bool("delete", false, "remove the device's voice; its calls go back to the global voice")
	removeNoise := fs.Bool("remove-background-noise", false, "ask ElevenLabs to isolate the voice first (can deform a clean recording's timbre)")
	description := fs.String("description", "", "description of the voice in ElevenLabs")
	name := fs.String("name", "", "ElevenLabs voice name (default mama-<first 8 characters of the device id>)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "setvoice: "+format+"\n", a...)
		return 1
	}
	if *device == "" || (*file == "") == !*del {
		fmt.Fprintln(stderr, "usage: setvoice --device <id> (--file <audio> | --delete)")
		fs.PrintDefaults()
		return 2
	}
	var missing []string
	for _, k := range []string{"DATABASE_URL", "ELEVENLABS_API_KEY"} {
		if getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fail("%s not set", strings.Join(missing, " and "))
	}

	var sample voice.Sample
	if !*del {
		audio, err := readAudio(*file)
		if err != nil {
			return fail("%v", err)
		}
		format, ok := voice.Sniff(audio)
		if !ok {
			return fail("%s is not mp3, wav, m4a, ogg or webm", *file)
		}
		sample = voice.Sample{Audio: audio, Format: format}
	}

	pool, err := db.Open(getenv("DATABASE_URL"))
	if err != nil {
		return fail("open database: %v", err)
	}
	defer pool.Close()
	svc := voice.NewService(voice.NewDBStore(pool), &voice.Client{BaseURL: elevenLabsURL, APIKey: getenv("ELEVENLABS_API_KEY")})

	if *del {
		if err := svc.Remove(ctx, *device); err != nil {
			return fail("%v", explain(err))
		}
		fmt.Fprintf(stdout, "device %s: voice removed; its calls use the global voice\n", *device)
		return 0
	}
	voiceID, err := svc.Set(ctx, *device, voice.NewVoice{Name: *name, Description: *description, RemoveBackgroundNoise: *removeNoise}, sample)
	if err != nil {
		return fail("%v", explain(err))
	}
	sum := sha256.Sum256(sample.Audio)
	fmt.Fprintf(stdout, "device %s: voice_id %s (consent recorded, audio sha256 %x; the audio was not stored)\n", *device, voiceID, sum)
	return 0
}

// readAudio reads the file, refusing one over voice.MaxAudioBytes.
func readAudio(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	audio, err := io.ReadAll(io.LimitReader(f, voice.MaxAudioBytes+1))
	if err != nil {
		return nil, err
	}
	if len(audio) > voice.MaxAudioBytes {
		return nil, fmt.Errorf("%s is over 10 MB: trim it (go run ./cmd/voiceprep)", path)
	}
	return audio, nil
}

// explain adds what to do to the errors an operator can fix.
func explain(err error) error {
	switch {
	case errors.Is(err, voice.ErrUnknownDevice):
		return fmt.Errorf("%w: register the device first (POST /devices)", err)
	case errors.Is(err, voice.ErrAuth):
		return fmt.Errorf("%w (README \"Secrets\")", err)
	}
	return err
}
