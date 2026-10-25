package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func needFFmpeg(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on the PATH", bin)
		}
	}
}

// synth writes a stereo 48 kHz file from an ffmpeg lavfi source.
func synth(t *testing.T, path, lavfi string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", lavfi, "-ac", "2", "-ar", "48000", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synth %s: %v\n%s", path, err, out)
	}
}

// loudness measures integrated loudness (LUFS) and true peak (dBFS).
func loudness(t *testing.T, path string) (lufs, peak float64) {
	t.Helper()
	out, _ := exec.Command("ffmpeg", "-hide_banner", "-nostats", "-i", path, "-af", "ebur128=peak=true", "-f", "null", "-").CombinedOutput()
	s := string(out)
	i := strings.LastIndex(s, "Summary:")
	if i < 0 {
		t.Fatalf("no ebur128 summary:\n%s", s)
	}
	s = s[i:]
	num := func(re string) float64 {
		m := regexp.MustCompile(re).FindStringSubmatch(s)
		if m == nil {
			t.Fatalf("%s not in:\n%s", re, s)
		}
		v, _ := strconv.ParseFloat(m[1], 64)
		return v
	}
	return num(`I:\s+(-?[\d.]+) LUFS`), num(`Peak:\s+(-?[\d.]+) dBFS`)
}

func probe(t *testing.T, path, entry string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream="+entry, "-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestPrepJoinsTrimsNormalizes(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	if err := os.Mkdir(in, 0o755); err != nil {
		t.Fatal(err)
	}
	// 01: 5 s tone, 3 s silence, 5 s tone, quiet; 02: 4 s tone, loud. The
	// sine source is at 1/8 of full scale before volume.
	synth(t, filepath.Join(in, "01.wav"), "sine=f=220:d=5,volume=0.5,apad=pad_dur=3[a];sine=f=330:d=5,volume=0.5[b];[a][b]concat=n=2:v=0:a=1")
	synth(t, filepath.Join(in, "02.mp3"), "sine=f=440:d=4,volume=7")
	if err := os.WriteFile(filepath.Join(in, "notes.txt"), []byte("not audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "muestra.mp3")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--in", in, "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	got, err := duration(out)
	if err != nil {
		t.Fatal(err)
	}
	// 5 + 0.7 (the 3 s silence shortened) + 5 + 4.
	if got < 14*time.Second || got > 15500*time.Millisecond {
		t.Fatalf("length %s, want about 14.7 s", got)
	}
	if ch, rate := probe(t, out, "channels"), probe(t, out, "sample_rate"); ch != "1" || rate != "44100" {
		t.Fatalf("channels %s, sample rate %s; want mono 44100", ch, rate)
	}
	lufs, peak := loudness(t, out)
	if lufs < -22 || lufs > -18 || peak > -3 {
		t.Fatalf("loudness %.1f LUFS, peak %.1f dBFS; want about -20 LUFS and under -3 dBFS", lufs, peak)
	}
	if !strings.Contains(stdout.String(), "from 2 file(s)") {
		t.Fatalf("stdout %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "warning: under 1m0s") {
		t.Fatalf("a 15 s sample must warn: %q", stdout.String())
	}
}

func TestPrepCutsAtThreeMinutes(t *testing.T) {
	needFFmpeg(t)
	dir := t.TempDir()
	long := filepath.Join(dir, "long.wav")
	synth(t, long, "sine=f=200:d=200,volume=0.3")
	out := filepath.Join(dir, "muestra.wav")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--out", out, "--denoise", long}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	got, err := duration(out)
	if err != nil {
		t.Fatal(err)
	}
	if got < maxLength-time.Second || got > maxLength+time.Second {
		t.Fatalf("length %s, want %s", got, maxLength)
	}
	if strings.Contains(stdout.String(), "warning") {
		t.Fatalf("a 3 min sample must not warn: %q", stdout.String())
	}
}

func TestPrepWithoutFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--in", "x.mp3"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "ffmpeg is not on the PATH") {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
}

func TestFFmpegArgs(t *testing.T) {
	args := strings.Join(ffmpegArgs([]string{"a.m4a", "b.ogg"}, "out.mp3", false), " ")
	for _, want := range []string{
		"-i a.m4a -i b.ogg",
		"concat=n=2:v=0:a=1",
		"stop_duration=0.7:stop_silence=0.7",
		"atrim=end=180",
		"loudnorm=I=-20:TP=-3",
		"-ac 1 -ar 44100 -c:a libmp3lame",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "afftdn") {
		t.Error("noise reduction must be off by default")
	}
	if !strings.Contains(strings.Join(ffmpegArgs([]string{"a"}, "o.wav", true), " "), "afftdn") {
		t.Error("--denoise must add afftdn")
	}
}
