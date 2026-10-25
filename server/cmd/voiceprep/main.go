// Command voiceprep builds the sample a "Mamá" voice is cloned from (#107),
// with ffmpeg (it must be on the PATH):
//
//	go run ./cmd/voiceprep --in <folder or file> [--in <file> ...] --out muestra.mp3 [--denoise]
//
// It joins the files (a folder's audio files in name order), shortens every
// silence over 0.7 s to 0.7 s, makes it mono at 44.1 kHz, normalizes the
// loudness to about -20 LUFS with peaks under -3 dBFS, and cuts it at 3
// minutes: more audio does not improve an instant clone. It prints the
// final length and warns under 60 s. Noise reduction is off unless
// --denoise, because it deforms the timbre.
//
// Then: go run ./cmd/setvoice --device <id> --file muestra.mp3
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Settings of the sample, from the ticket.
const (
	maxLength    = 3 * time.Minute
	minLength    = 60 * time.Second
	maxSilence   = "0.7"   // seconds kept of a longer silence
	silenceFloor = "-45dB" // quieter than this is silence
	targetLUFS   = "-20"
	peakDBFS     = "-3"
	// peakLimit is -3.2 dBFS as a linear amplitude, so the encoded file
	// stays under -3 dBFS.
	peakLimit = "0.69"
)

// audioExts are the files taken from a folder.
var audioExts = map[string]bool{
	".mp3": true, ".wav": true, ".m4a": true, ".ogg": true, ".webm": true,
	".flac": true, ".aac": true, ".opus": true, ".mp4": true,
}

type inputs []string

func (i *inputs) String() string     { return strings.Join(*i, ",") }
func (i *inputs) Set(v string) error { *i = append(*i, v); return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("voiceprep", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var in inputs
	fs.Var(&in, "in", "a folder of audio files or one file; repeat for more (files may also follow the flags)")
	out := fs.String("out", "muestra.mp3", "the sample to write (.mp3 or .wav)")
	denoise := fs.Bool("denoise", false, "apply FFT noise reduction (off: it deforms the timbre)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	in = append(in, fs.Args()...)
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "voiceprep: "+format+"\n", a...)
		return 1
	}
	if len(in) == 0 {
		fmt.Fprintln(stderr, "usage: voiceprep --in <folder or file> [--in ...] --out muestra.mp3 [--denoise]")
		return 2
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fail("ffmpeg is not on the PATH: install it (https://ffmpeg.org/download.html) and try again")
	}
	files, err := expand(in)
	if err != nil {
		return fail("%v", err)
	}
	cmd := exec.Command(ffmpeg, ffmpegArgs(files, *out, *denoise)...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fail("ffmpeg failed: %v\n%s", err, tail(msg))
	}
	length, err := duration(*out)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Fprintf(stdout, "%s: %s from %d file(s)\n", *out, length.Round(100*time.Millisecond), len(files))
	if length < minLength {
		fmt.Fprintf(stdout, "warning: under %s; an instant clone wants 1 to 2 minutes of clean speech\n", minLength)
	}
	return 0
}

// expand turns folders into their audio files, in name order.
func expand(in []string) ([]string, error) {
	var files []string
	for _, p := range in {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			files = append(files, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		var found []string
		for _, e := range entries {
			if !e.IsDir() && audioExts[strings.ToLower(filepath.Ext(e.Name()))] {
				found = append(found, filepath.Join(p, e.Name()))
			}
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%s has no audio files", p)
		}
		sort.Strings(found)
		files = append(files, found...)
	}
	return files, nil
}

// ffmpegArgs is the whole pipeline in one ffmpeg run.
func ffmpegArgs(files []string, out string, denoise bool) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	var graph strings.Builder
	for i, f := range files {
		args = append(args, "-i", f)
		fmt.Fprintf(&graph, "[%d:a:0]aresample=44100,aformat=sample_fmts=fltp:channel_layouts=mono[a%d];", i, i)
	}
	for i := range files {
		fmt.Fprintf(&graph, "[a%d]", i)
	}
	fmt.Fprintf(&graph, "concat=n=%d:v=0:a=1,", len(files))
	// Leading silence goes; every later one over maxSilence keeps maxSilence.
	graph.WriteString("silenceremove=start_periods=1:start_threshold=" + silenceFloor +
		":stop_periods=-1:stop_duration=" + maxSilence + ":stop_silence=" + maxSilence + ":stop_threshold=" + silenceFloor + ",")
	if denoise {
		graph.WriteString("afftdn=nf=-25,")
	}
	graph.WriteString("atrim=end=" + strconv.Itoa(int(maxLength.Seconds())) + ",")
	graph.WriteString("loudnorm=I=" + targetLUFS + ":TP=" + peakDBFS + ":LRA=11,")
	graph.WriteString("aresample=44100,alimiter=limit=" + peakLimit + ":level=disabled[out]")
	args = append(args, "-filter_complex", graph.String(), "-map", "[out]", "-ac", "1", "-ar", "44100")
	if strings.EqualFold(filepath.Ext(out), ".wav") {
		args = append(args, "-c:a", "pcm_s16le")
	} else {
		args = append(args, "-c:a", "libmp3lame", "-b:a", "192k")
	}
	return append(args, out)
}

// duration reads a file's length with ffprobe.
func duration(path string) (time.Duration, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0, errors.New("ffprobe is not on the PATH (it comes with ffmpeg)")
	}
	raw, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe %s: %q is not a duration", path, raw)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// tail keeps the end of ffmpeg's output, where the error is.
func tail(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 1500 {
		s = "…" + s[len(s)-1500:]
	}
	return s
}
