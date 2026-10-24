// Command voicelab compares TTS settings for a cloned "Mamá" voice (#107):
// it has ElevenLabs text-to-speech say Mamá's real lines for a grid of
// settings, and writes each one twice into voicelab-out/ (gitignored): as
// generated, and at phone quality (8 kHz, μ-law, 300 to 3400 Hz, as Twilio
// carries it). An index.html there plays them side by side with the
// original sample.
//
//	go run ./cmd/voicelab --voice <voice_id> [--text lines.txt] [--original muestra.mp3]
//	                      [--models eleven_flash_v2_5,...] [--lines 3] [--dry-run]
//
// Grid: every model × stability 0.35, 0.5, 0.65 × similarity_boost 0.75,
// 0.9 × use_speaker_boost on and off, at tts.json's speed. It prints the
// characters and estimated credits before calling, and skips files that
// already exist, so a second run costs nothing. Copy the winner's
// model_id, stability and similarity_boost to
// server/internal/call/agent/tts.json (the agent has no use_speaker_boost).
// It reads ELEVENLABS_API_KEY; ffmpeg on the PATH makes the phone versions.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/call/agent"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/voice"
)

// defaultLines are what Mamá says in a call (call/agent/prompt.md).
var defaultLines = []string{
	"¡Hola, Mari! ¿Cómo vas, mi amor?",
	"¿Y por dónde vas ya?",
	"Bueno, te espero. Bendición.",
}

// defaultModels are the agent's Spanish models (call/agent/tts.json) that
// the text-to-speech API takes too; --models adds the others.
var defaultModels = []string{"eleven_flash_v2_5", "eleven_turbo_v2_5", "eleven_multilingual_v2"}

var (
	stabilities  = []float64{0.35, 0.5, 0.65}
	similarities = []float64{0.75, 0.9}
	boosts       = []bool{true, false}
)

// creditsPerChar is ElevenLabs' price per character: Flash and Turbo cost
// half a credit, the rest one.
func creditsPerChar(model string) float64 {
	if strings.Contains(model, "flash") || strings.Contains(model, "turbo") {
		return 0.5
	}
	return 1
}

// combo is one point of the grid.
type combo struct {
	Model      string
	Stability  float64
	Similarity float64
	Boost      bool
}

func (c combo) slug() string {
	b := 0
	if c.Boost {
		b = 1
	}
	return fmt.Sprintf("%s_st%03.0f_sim%03.0f_boost%d", c.Model, c.Stability*100, c.Similarity*100, b)
}

func grid(models []string) []combo {
	var out []combo
	for _, m := range models {
		for _, st := range stabilities {
			for _, sim := range similarities {
				for _, b := range boosts {
					out = append(out, combo{m, st, sim, b})
				}
			}
		}
	}
	return out
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, voice.BaseURL))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer, elevenLabsURL string) int {
	fs := flag.NewFlagSet("voicelab", flag.ContinueOnError)
	fs.SetOutput(stderr)
	voiceID := fs.String("voice", "", "voice_id to try (required)")
	textFile := fs.String("text", "", "file with one line per row (default: Mamá's three lines)")
	original := fs.String("original", "", "the sample the voice was cloned from, to listen to next to the grid")
	models := fs.String("models", strings.Join(defaultModels, ","), "comma-separated model ids")
	maxLines := fs.Int("lines", 0, "use only the first N lines (0: all)")
	outDir := fs.String("out", "voicelab-out", "output folder")
	dryRun := fs.Bool("dry-run", false, "print the grid and its cost, call nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "voicelab: "+format+"\n", a...)
		return 1
	}
	if *voiceID == "" {
		fmt.Fprintln(stderr, "usage: voicelab --voice <voice_id> [--text lines.txt] [--original muestra.mp3] [--models a,b] [--lines N] [--dry-run]")
		return 2
	}
	lines := defaultLines
	if *textFile != "" {
		var err error
		if lines, err = readLines(*textFile); err != nil {
			return fail("%v", err)
		}
	}
	if *maxLines > 0 && *maxLines < len(lines) {
		lines = lines[:*maxLines]
	}
	var modelIDs []string
	for _, m := range strings.Split(*models, ",") {
		if m = strings.TrimSpace(m); m != "" {
			modelIDs = append(modelIDs, m)
		}
	}
	if len(modelIDs) == 0 {
		return fail("--models is empty")
	}
	tts, err := agent.LoadTTS()
	if err != nil {
		return fail("%v", err)
	}
	combos := grid(modelIDs)

	chars := 0
	for _, l := range lines {
		chars += len([]rune(l))
	}
	var credits float64
	for _, c := range combos {
		credits += float64(chars) * creditsPerChar(c.Model)
	}
	fmt.Fprintf(stdout, "grid: %d settings × %d lines = %d requests, %d characters, about %.0f credits (before skipping files that exist)\n",
		len(combos), len(lines), len(combos)*len(lines), chars*len(combos), credits)
	if *dryRun {
		return 0
	}
	key := getenv("ELEVENLABS_API_KEY")
	if key == "" {
		return fail("ELEVENLABS_API_KEY not set")
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fail("%v", err)
	}
	ffmpeg, ffErr := exec.LookPath("ffmpeg")
	if ffErr != nil {
		fmt.Fprintln(stderr, "voicelab: ffmpeg is not on the PATH: skipping the phone-quality versions")
	}

	page := indexPage{Voice: *voiceID, Lines: lines, Speed: tts.Speed, Phone: ffErr == nil}
	if *original != "" {
		name := "original" + filepath.Ext(*original)
		if err := copyFile(*original, filepath.Join(*outDir, name)); err != nil {
			return fail("%v", err)
		}
		page.Original = name
		if ffErr == nil {
			if err := phone(ffmpeg, filepath.Join(*outDir, name), filepath.Join(*outDir, "original_tel.wav")); err != nil {
				return fail("%v", err)
			}
			page.OriginalPhone = "original_tel.wav"
		}
	}

	client := &voice.Client{BaseURL: elevenLabsURL, APIKey: key}
	spent := 0.0
	for _, c := range combos {
		row := indexRow{combo: c}
		for i, line := range lines {
			base := fmt.Sprintf("%s_l%d", c.slug(), i+1)
			mp3 := filepath.Join(*outDir, base+".mp3")
			if _, err := os.Stat(mp3); errors.Is(err, os.ErrNotExist) {
				boost := c.Boost
				audio, err := client.Speak(ctx, voice.Speech{
					VoiceID:  *voiceID,
					Text:     line,
					ModelID:  c.Model,
					Settings: voice.Settings{Stability: c.Stability, SimilarityBoost: c.Similarity, UseSpeakerBoost: &boost, Speed: tts.Speed},
				})
				if err != nil {
					return fail("%s line %d: %v", c.slug(), i+1, err)
				}
				if err := os.WriteFile(mp3, audio, 0o644); err != nil {
					return fail("%v", err)
				}
				spent += float64(len([]rune(line))) * creditsPerChar(c.Model)
			}
			cell := indexCell{Normal: base + ".mp3"}
			if ffErr == nil {
				cell.Phone = base + "_tel.wav"
				if _, err := os.Stat(filepath.Join(*outDir, cell.Phone)); errors.Is(err, os.ErrNotExist) {
					if err := phone(ffmpeg, mp3, filepath.Join(*outDir, cell.Phone)); err != nil {
						return fail("%v", err)
					}
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		page.Rows = append(page.Rows, row)
		fmt.Fprintf(stdout, "  %s\n", c.slug())
	}
	index := filepath.Join(*outDir, "index.html")
	f, err := os.Create(index)
	if err != nil {
		return fail("%v", err)
	}
	if err := indexTemplate.Execute(f, page); err != nil {
		f.Close()
		return fail("index.html: %v", err)
	}
	if err := f.Close(); err != nil {
		return fail("%v", err)
	}
	fmt.Fprintf(stdout, "this run: about %.0f credits; open %s\n", spent, index)
	return 0
}

// phone renders in at phone quality: 300-3400 Hz, 8 kHz, μ-law, then back
// to 16-bit PCM so a browser plays it (browsers do not play μ-law WAV).
func phone(ffmpeg, in, out string) error {
	ulaw, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", in,
		"-af", "highpass=f=300,lowpass=f=3400", "-ar", "8000", "-ac", "1", "-c:a", "pcm_mulaw", "-f", "wav", "-").Output()
	if err != nil {
		return fmt.Errorf("ffmpeg phone %s: %w", in, err)
	}
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "wav", "-i", "-", "-c:a", "pcm_s16le", out)
	cmd.Stdin = bytes.NewReader(ulaw)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg phone %s: %w: %s", in, err, msg)
	}
	return nil
}

func readLines(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%s has no lines", path)
	}
	return lines, nil
}

func copyFile(from, to string) error {
	raw, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, raw, 0o644)
}

type indexPage struct {
	Voice         string
	Lines         []string
	Speed         float64
	Phone         bool
	Original      string
	OriginalPhone string
	Rows          []indexRow
}

type indexRow struct {
	combo
	Cells []indexCell
}

type indexCell struct{ Normal, Phone string }

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Voicelab</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 16px; background: #fff; color: #1a1a1a; }
  table { border-collapse: collapse; }
  th, td { border: 1px solid #ddd; padding: 6px 8px; vertical-align: top; font-size: 14px; }
  th { background: #f4f4f4; position: sticky; top: 0; }
  audio { width: 220px; height: 32px; display: block; }
  .tag { font-size: 12px; color: #555; }
  tr.pick { background: #fff7d6; }
  code { background: #f4f4f4; padding: 1px 4px; }
</style>
</head>
<body>
<h1>Voicelab: <code>{{.Voice}}</code></h1>
<p>Velocidad {{.Speed}} (de <code>tts.json</code>). Marca una fila para recordarla; el ganador va a
<code>server/internal/call/agent/tts.json</code> (<code>model_id</code>, <code>stability</code>,
<code>similarity_boost</code>). El agente no tiene <code>use_speaker_boost</code>: esa columna solo
cambia este texto a voz.</p>
{{if .Original}}<h2>Original</h2>
<table><tr><th>Normal</th>{{if .OriginalPhone}}<th>Teléfono</th>{{end}}</tr>
<tr><td><audio controls preload="none" src="{{.Original}}"></audio></td>
{{if .OriginalPhone}}<td><audio controls preload="none" src="{{.OriginalPhone}}"></audio></td>{{end}}</tr></table>{{end}}
<h2>Grilla</h2>
<table>
<tr><th></th><th>Modelo</th><th>stability</th><th>similarity</th><th>speaker boost</th>
{{range .Lines}}<th>{{.}}{{if $.Phone}}<div class="tag">normal / teléfono</div>{{end}}</th>{{end}}</tr>
{{range .Rows}}<tr>
<td><input type="checkbox" onchange="this.closest('tr').classList.toggle('pick', this.checked)"></td>
<td>{{.Model}}</td><td>{{.Stability}}</td><td>{{.Similarity}}</td><td>{{if .Boost}}sí{{else}}no{{end}}</td>
{{range .Cells}}<td><audio controls preload="none" src="{{.Normal}}"></audio>{{if .Phone}}<audio controls preload="none" src="{{.Phone}}"></audio>{{end}}</td>{{end}}
</tr>{{end}}
</table>
</body>
</html>
`))
