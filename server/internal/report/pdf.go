package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/duress"
)

// The detailed report as a PDF (GET /i/{token}/reporte.pdf): what a family
// sends to the CICPC or the prosecutor. Once sealed it is built from the
// stored ficha.json, so every fact in it is a fact the seal covers; before
// that it is marked "Borrador, sin sellar". The family messages and linked
// offline emergencies are added from the live record and say they are not
// part of the seal.

// pdfReport is the PDF's content, built by buildPDF (pure, tested) and drawn
// by renderPDF.
type pdfReport struct {
	Draft       bool
	IncidentID  string
	Title       string // the verdict headline
	Triggered   string // long date of the gesture
	Facts       [][2]string
	Timeline    []pdfEvent
	Points      []pdfPoint
	Phone       *pdfPhone // the phone's permissions (#125); nil when it sent none
	Calls       []pdfCall
	Contexts    []pdfContext // the calls' contexts (#106); none without a ficha
	Family      []pdfFamily
	FamilyNote  string
	Emergencies []string
	Seal        [][2]string // label, value; empty while a draft
	SealNote    string      // why it is a draft
	Verify      []string
	Honest      string
	Generated   string
	Links       [][2]string // label, URL of the report's files
}

type pdfEvent struct{ Time, Clock, Title, Detail string }

type pdfPoint struct{ N, Time, LatLng, Accuracy, Speed, Maps string }

type pdfCall struct {
	Title string
	Facts [][2]string
	Turns []pdfTurn
}

// pdfContext is one call's context: its fields that have a value.
type pdfContext struct {
	Title  string
	Fields [][2]string
}

type pdfTurn struct {
	Speaker, Offset, Text string
	Phrase                bool
}

type pdfFamily struct{ Recipient, Status, Time, Error string }

// pdfPhone is "Preparación del teléfono" (#125): the permissions at the
// gesture and how they were granted. Read from the live record: it is not
// part of the seal.
type pdfPhone struct {
	Ready *PhoneReady
	Prep  []PrepStep
}

// PDFOptions are the PDF settings that do not come from the record.
type PDFOptions struct {
	Token   string
	BaseURL string // the server's public URL, for the file links; blank gives paths
	Channel string
	Now     time.Time
}

// honestText is what the seal proves, and what it does not.
const honestText = "El sello prueba que el registro no cambió desde que el servidor de Aló Mamá lo cerró. " +
	"No es una firma electrónica certificada ni reemplaza una experticia."

// buildPDF gathers the PDF's content. Sealed, it reads the incident, points
// and calls back from the signed ficha.json.
func buildPDF(rec Record, opts PDFOptions) (pdfReport, error) {
	src := rec
	if rec.Seal != nil {
		var err error
		if src, err = recordFromDocument(rec.Seal.Document, rec); err != nil {
			return pdfReport{}, fmt.Errorf("sealed document: %w", err)
		}
	}
	inc := src.Incident
	r := pdfReport{
		Draft:      rec.Seal == nil,
		IncidentID: inc.ID,
		Triggered:  longDate(inc.TriggeredAt),
		Generated:  longClock(opts.Now),
		Honest:     honestText,
	}
	r.Title, _ = headline(src)
	r.Facts = [][2]string{
		{"Incidente", inc.ID},
		{"Quién", nonEmpty(rec.Device.UserName, "sin nombre registrado")},
		{"Pidió ayuda", "el " + longDate(inc.TriggeredAt) + ", " + triggerLabel(inc.Trigger) + " (reloj del teléfono)"},
		{"Llegó al servidor", longClock(inc.CreatedAt) + " (" + receivedLabel(inc.CreatedAt.Sub(inc.TriggeredAt)) + ")"},
		{"Qué hizo Aló Mamá", branchLabel(inc.Branch, inc.SpeedMPS)},
		{"Teléfono", maskPhone(rec.Device.PhoneNumber)},
		{"Familiares registrados", strconv.Itoa(src.Device.FamilyNumbers)},
	}
	for _, e := range Timeline(src, Options{Channel: opts.Channel}) {
		d := e.Detail
		if e.Quote != "" {
			d = strings.TrimSpace(d + " «" + e.Quote + "»")
		}
		r.Timeline = append(r.Timeline, pdfEvent{Time: e.Time(), Clock: e.Clock.Label(), Title: e.Title, Detail: d})
	}
	for i, p := range src.Points {
		r.Points = append(r.Points, pdfPoint{
			N: strconv.Itoa(i + 1), Time: clock(p.RecordedAt), LatLng: fmt.Sprintf("%.5f, %.5f", p.Lat, p.Lng),
			Accuracy: accuracy(p.AccuracyM), Speed: movement(p.SpeedMPS), Maps: MapsURL(p.Lat, p.Lng),
		})
	}
	if ready, prep := phoneReady(rec), prepSteps(rec); ready != nil || len(prep) > 0 {
		r.Phone = &pdfPhone{Ready: ready, Prep: prep}
	}
	for i, c := range src.Calls {
		r.Calls = append(r.Calls, pdfCallOf(i+1, len(src.Calls), c, src.Device))
		if c.Context != nil {
			title := "Contexto de la llamada"
			if len(src.Calls) > 1 {
				title += " " + strconv.Itoa(i+1)
			}
			r.Contexts = append(r.Contexts, pdfContext{Title: title, Fields: c.Context.Fields()})
		}
	}
	for _, d := range rec.Deliveries {
		f := pdfFamily{Recipient: d.Recipient, Error: d.Error}
		switch d.Status {
		case "read":
			f.Status = "leído"
			if d.ReadAt != nil {
				f.Time = clock(*d.ReadAt)
			}
		case "delivered":
			f.Status = "entregado"
			if d.DeliveredAt != nil {
				f.Time = clock(*d.DeliveredAt)
			}
		case "failed":
			f.Status, f.Time = "no se envió", clock(d.SentAt)
		default:
			f.Status, f.Time = "enviado", clock(d.SentAt)
		}
		r.Family = append(r.Family, f)
	}
	if len(r.Family) == 0 {
		r.FamilyNote = familyNote(rec, opts.Channel, false)
	}
	for _, e := range emergencyViews(rec.Emergencies) {
		r.Emergencies = append(r.Emergencies, fmt.Sprintf("Emergencia %s (%s): llegó por %s a las %s (servidor).", e.ID, e.Status, e.Channels, e.Received))
	}

	base := strings.TrimSuffix(opts.BaseURL, "/")
	if s := rec.Seal; s != nil {
		doc := sha256.Sum256(s.Document)
		r.Seal = [][2]string{
			{"Sellada el", longClock(s.SealedAt)},
			{"SHA-256 de ficha.json", hex.EncodeToString(doc[:])},
			{"Firma Ed25519 (ficha.sig)", base64.StdEncoding.EncodeToString(s.Signature)},
			{"Llave pública del servidor", base64.StdEncoding.EncodeToString(s.PublicKey)},
			{"Huella de la llave (SHA-256)", Fingerprint(s.PublicKey)},
			{"Contexto firmado", SignatureContext + " || ficha.json"},
		}
		r.Links = [][2]string{
			{"ficha.json", base + DocumentPath(opts.Token)},
			{"ficha.sig", base + SignaturePath(opts.Token)},
		}
		r.Verify = []string{
			"1. Descarga ficha.json y ficha.sig de los enlaces de arriba.",
			"2. Toma la llave pública del servidor de esta página o de su GET /config.",
			"3. En el repositorio de Aló Mamá, carpeta server: go run ./cmd/verify-ficha ficha.json ficha.sig " + base64.StdEncoding.EncodeToString(s.PublicKey),
			"4. Debe responder «firma válida». Si cambió un solo byte, dice que no.",
		}
	} else {
		r.SealNote = sealView(rec, Options{CanSeal: true}).Why
	}
	return r, nil
}

func pdfCallOf(n, total int, c Call, dev Device) pdfCall {
	pc := pdfCall{Title: "Llamada"}
	if total > 1 {
		pc.Title = "Llamada " + strconv.Itoa(n)
		if n > 1 {
			pc.Title += " (reintento)"
		}
	}
	pc.Facts = [][2]string{{"Pedida", clock(c.RequestedAt) + " (servidor)"}}
	if a, ok := c.AnsweredAt(); ok {
		pc.Facts = append(pc.Facts, [2]string{"Contestó", clock(a) + " (ElevenLabs)"})
	}
	if c.EndedAt != nil {
		pc.Facts = append(pc.Facts, [2]string{"Terminó", clock(*c.EndedAt)})
	}
	status := map[string]string{"done": "terminó", "failed": "falló", "requested": "pedida", "in_progress": "en curso"}[c.Status]
	if c.DurationSecs != nil {
		status += ", " + duration(time.Duration(*c.DurationSecs)*time.Second)
	}
	pc.Facts = append(pc.Facts, [2]string{"Estado", status})
	if c.ConversationID != "" {
		pc.Facts = append(pc.Facts, [2]string{"Conversación (ElevenLabs)", c.ConversationID})
	}
	if c.Verdict != "" {
		pc.Facts = append(pc.Facts, [2]string{"Veredicto", verdictLine(c.Verdict, c.VerdictReason)})
		if c.VerdictReason != "" {
			pc.Facts = append(pc.Facts, [2]string{"Motivo del detector", c.VerdictReason})
		}
	}
	if r := c.Recording; r != nil {
		pc.Facts = append(pc.Facts,
			[2]string{"Grabación", size(r.Size) + ", " + r.ContentType + ", guardada a las " + clock(r.StoredAt)},
			[2]string{"SHA-256 de la grabación", hex.EncodeToString(r.SHA256[:])})
	}
	phrase := phraseTurns(c.Transcript, dev.DuressPhrase)
	for i, t := range c.Transcript {
		sp := "Tu familiar"
		if t.Role == duress.RoleAgent {
			sp = "Mamá"
		}
		pc.Turns = append(pc.Turns, pdfTurn{Speaker: sp, Offset: offset(t.TimeInCallSecs), Text: t.Message, Phrase: phrase[i]})
	}
	return pc
}

// recordFromDocument reads a v1 ficha.json back into a Record, keeping from
// live only what the document leaves out on purpose: the device id (never
// shown), the device's numbers (shown masked) and the duress phrase (to mark
// its turn).
func recordFromDocument(raw []byte, live Record) (Record, error) {
	var d document
	if err := json.Unmarshal(raw, &d); err != nil {
		return Record{}, err
	}
	if d.Format != SignatureContext {
		return Record{}, fmt.Errorf("format %q", d.Format)
	}
	parse := func(s string) time.Time {
		t, _ := time.Parse(time.RFC3339Nano, s)
		return t
	}
	parsePtr := func(s string) *time.Time {
		if s == "" {
			return nil
		}
		t := parse(s)
		return &t
	}
	rec := Record{
		Incident: Incident{
			ID: d.Incident.IncidentID, DeviceID: live.Incident.DeviceID,
			Trigger: contract.Trigger(d.Incident.Trigger), Branch: contract.Branch(d.Incident.Branch),
			SpeedMPS: d.Incident.SpeedMPS, TriggeredAt: parse(d.Incident.TriggeredAt),
			CreatedAt: parse(d.Incident.ReceivedAt), NotifiedAt: parsePtr(d.Incident.NotifiedAt),
		},
		Device: Device{PhoneNumber: live.Device.PhoneNumber, FamilyNumbers: d.Incident.FamilyNumbers, DuressPhrase: live.Device.DuressPhrase},
		Seal:   live.Seal,
	}
	for _, p := range d.Points {
		rec.Points = append(rec.Points, Point{Lat: p.Lat, Lng: p.Lng, AccuracyM: p.AccuracyM, SpeedMPS: p.SpeedMPS,
			RecordedAt: parse(p.RecordedAt), ReceivedAt: parse(p.ReceivedAt)})
	}
	for _, c := range d.Calls {
		call := Call{
			ConversationID: c.ConversationID, Status: c.Status, Transcript: c.Transcript, DurationSecs: c.DurationSecs,
			RequestedAt: parse(c.RequestedAt), EndedAt: parsePtr(c.EndedAt),
			Verdict: contract.Verdict(c.Verdict), VerdictReason: c.VerdictReason, VerdictAt: parsePtr(c.VerdictAt),
		}
		if c.Context != nil {
			ctx := *c.Context
			call.Context = &ctx
		}
		if r := c.Recording; r != nil {
			rr := &Recording{ContentType: r.ContentType, Size: r.SizeBytes, StoredAt: parse(r.StoredAt)}
			if sum, err := hex.DecodeString(r.SHA256); err == nil && len(sum) == 32 {
				copy(rr.SHA256[:], sum)
			}
			call.Recording = rr
		}
		rec.Calls = append(rec.Calls, call)
	}
	return rec, nil
}

// ------------------------------------------------------------------ drawing

// Palette: the logo's charcoal and chartreuse, Olive for headings
// (Theme.kt), ink and graphite for text.
var (
	pdfCharcoal = [3]int{0x18, 0x19, 0x18}
	pdfLime     = [3]int{0xEF, 0xFE, 0x00}
	pdfOlive    = [3]int{0x4A, 0x54, 0x00}
	pdfLimeSoft = [3]int{0xE9, 0xF6, 0x8C}
	pdfInk      = [3]int{0x1B, 0x1C, 0x16}
	pdfGraphite = [3]int{0x5D, 0x5F, 0x52}
	pdfLine     = [3]int{0xC7, 0xC8, 0xB6}
	pdfPaper    = [3]int{0xF5, 0xF5, 0xEC}
)

// renderPDF draws the report on A4 with Lexend embedded (UTF-8, so accents
// and ñ come out right).
func renderPDF(r pdfReport) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCreationDate(time.Unix(0, 0).UTC())
	pdf.SetModificationDate(time.Unix(0, 0).UTC())
	pdf.SetTitle("Ficha del incidente · Aló Mamá", true)
	pdf.SetAuthor("Aló Mamá", true)
	pdf.SetProducer("Aló Mamá", true)
	for style, file := range map[string]string{"": "lexend_regular.ttf", "B": "lexend_bold.ttf", "I": "lexend_medium.ttf"} {
		b, err := fs.ReadFile(staticFS, "static/"+file)
		if err != nil {
			return nil, err
		}
		pdf.AddUTF8FontFromBytes("Lexend", style, b)
	}
	if b, err := fs.ReadFile(staticFS, "static/mama_mark.png"); err == nil {
		pdf.RegisterImageOptionsReader("mark", fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(b))
	}
	const margin = 16.0
	pageW, pageH := pdf.GetPageSize()
	textW := pageW - 2*margin
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(true, 18)
	pdf.AliasNbPages("{nb}")
	color := func(c [3]int) { pdf.SetTextColor(c[0], c[1], c[2]) }
	fill := func(c [3]int) { pdf.SetFillColor(c[0], c[1], c[2]) }
	draw := func(c [3]int) { pdf.SetDrawColor(c[0], c[1], c[2]) }
	status := "SELLADA"
	if r.Draft {
		status = "BORRADOR, SIN SELLAR"
	}
	pdf.SetFooterFunc(func() {
		pdf.SetY(-13)
		pdf.SetFont("Lexend", "", 8)
		color(pdfGraphite)
		pdf.CellFormat(textW/2, 5, "Aló Mamá · Ficha del incidente "+shortID(r.IncidentID)+" · "+strings.ToLower(status), "", 0, "L", false, 0, "")
		pdf.CellFormat(textW/2, 5, "Página "+strconv.Itoa(pdf.PageNo())+" de {nb}", "", 0, "R", false, 0, "")
	})
	pdf.AddPage()

	// Band: the logo's look, chartreuse on charcoal.
	fill(pdfCharcoal)
	pdf.Rect(0, 0, pageW, 34, "F")
	pdf.ImageOptions("mark", margin, 8, 18, 18, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	pdf.SetXY(margin+22, 9)
	pdf.SetFont("Lexend", "B", 20)
	color(pdfLime)
	pdf.CellFormat(60, 9, "ALÓ MAMÁ", "", 2, "L", false, 0, "")
	pdf.SetX(margin + 22)
	pdf.SetFont("Lexend", "", 10)
	pdf.SetTextColor(0xE5, 0xE5, 0xDB)
	pdf.CellFormat(90, 6, "Ficha del incidente", "", 0, "L", false, 0, "")
	pdf.SetFont("Lexend", "B", 9)
	sw := pdf.GetStringWidth(status) + 10
	if r.Draft {
		fill(pdfGraphite)
		pdf.SetTextColor(0xFF, 0xFF, 0xFF)
	} else {
		fill(pdfLime)
		color(pdfCharcoal)
	}
	pdf.RoundedRect(pageW-margin-sw, 12, sw, 8, 4, "1234", "F")
	pdf.SetXY(pageW-margin-sw, 12)
	pdf.CellFormat(sw, 8, status, "", 0, "C", false, 0, "")
	pdf.SetY(42)

	// Headline.
	color(pdfOlive)
	pdf.SetFont("Lexend", "B", 22)
	pdf.MultiCell(textW, 10, r.Title, "", "L", false)
	color(pdfGraphite)
	pdf.SetFont("Lexend", "", 10)
	pdf.MultiCell(textW, 5, "Pidió ayuda el "+r.Triggered+". Documento generado el "+r.Generated+".", "", "L", false)
	if r.Draft {
		pdf.Ln(2)
		fill(pdfPaper)
		color(pdfInk)
		pdf.MultiCell(textW, 5, "Borrador, sin sellar. "+r.SealNote+" Los datos pueden cambiar hasta que el servidor selle la ficha.", "", "L", true)
	}

	heading := func(s string) {
		pdf.Ln(5)
		if pdf.GetY() > pageH-50 {
			pdf.AddPage()
		}
		color(pdfOlive)
		pdf.SetFont("Lexend", "B", 12)
		pdf.CellFormat(textW, 7, strings.ToUpper(s), "", 1, "L", false, 0, "")
		draw(pdfLine)
		pdf.SetLineWidth(.3)
		pdf.Line(margin, pdf.GetY(), pageW-margin, pdf.GetY())
		pdf.Ln(2)
	}
	// row draws cells side by side, each wrapped in its width, as one row.
	row := func(cells []string, widths []float64, styles []string, size float64, link string) {
		lh := size * 0.48
		lines := 1
		for i, c := range cells {
			pdf.SetFont("Lexend", styles[i], size)
			if n := len(pdf.SplitText(c, widths[i]-2)); n > lines {
				lines = n
			}
		}
		h := float64(lines)*lh + 2
		if pdf.GetY()+h > pageH-20 {
			pdf.AddPage()
		}
		y, x := pdf.GetY(), margin
		for i, c := range cells {
			pdf.SetXY(x, y+1)
			pdf.SetFont("Lexend", styles[i], size)
			if link != "" && i == len(cells)-1 {
				color(pdfOlive)
				pdf.CellFormat(widths[i], lh, c, "", 0, "L", false, 0, link)
				color(pdfInk)
			} else {
				pdf.MultiCell(widths[i], lh, c, "", "L", false)
			}
			x += widths[i]
		}
		pdf.SetY(y + h)
		draw(pdfLine)
		pdf.SetLineWidth(.1)
		pdf.Line(margin, pdf.GetY(), pageW-margin, pdf.GetY())
	}
	facts := func(kv [][2]string) {
		for _, f := range kv {
			color(pdfGraphite)
			pdf.SetFont("Lexend", "", 9)
			y := pdf.GetY()
			pdf.MultiCell(52, 4.6, f[0], "", "L", false)
			y2 := pdf.GetY()
			pdf.SetXY(margin+52, y)
			color(pdfInk)
			pdf.SetFont("Lexend", "I", 9.5)
			pdf.MultiCell(textW-52, 4.6, f[1], "", "L", false)
			if y2 > pdf.GetY() {
				pdf.SetY(y2)
			}
			pdf.Ln(1)
		}
	}

	heading("El caso")
	facts(r.Facts)

	heading("Línea de tiempo")
	color(pdfGraphite)
	row([]string{"Hora (VET)", "Reloj", "Qué pasó"}, []float64{22, 24, textW - 46}, []string{"B", "B", "B"}, 8.5, "")
	color(pdfInk)
	for _, e := range r.Timeline {
		txt := e.Title
		if e.Detail != "" {
			txt += ": " + e.Detail
		}
		row([]string{e.Time, e.Clock, txt}, []float64{22, 24, textW - 46}, []string{"I", "", ""}, 9, "")
	}

	heading("Ubicaciones")
	if len(r.Points) == 0 {
		pdf.SetFont("Lexend", "", 9.5)
		pdf.MultiCell(textW, 5, "No llegó ninguna ubicación.", "", "L", false)
	} else {
		color(pdfGraphite)
		ws := []float64{8, 22, 46, 20, 42, textW - 138}
		row([]string{"#", "Hora", "Coordenadas", "Precisión", "Movimiento", "Mapa"}, ws, []string{"B", "B", "B", "B", "B", "B"}, 8.5, "")
		color(pdfInk)
		for _, p := range r.Points {
			row([]string{p.N, p.Time, p.LatLng, p.Accuracy, p.Speed, "Abrir en Maps"}, ws, []string{"", "I", "", "", "", "I"}, 9, p.Maps)
		}
	}

	if ph := r.Phone; ph != nil {
		heading("Preparación del teléfono")
		if rd := ph.Ready; rd != nil {
			color(pdfInk)
			pdf.SetFont("Lexend", "B", 9.5)
			line := "Al pedir ayuda (" + rd.Time + ", reloj del teléfono): " + strings.TrimSuffix(rd.Summary, " ✓") + " permisos."
			if rd.Missing != "" {
				line += " Falta: " + rd.Missing + "."
			}
			pdf.MultiCell(textW, 5, line, "", "L", false)
			pdf.Ln(1)
			kv := make([][2]string, 0, len(rd.Chips))
			for _, c := range rd.Chips {
				v := "sí"
				if !c.OK {
					v = "no"
				}
				kv = append(kv, [2]string{permissionTexts[c.Name].On, v})
			}
			facts(kv)
		}
		if len(ph.Prep) > 0 {
			pdf.Ln(1)
			color(pdfGraphite)
			row([]string{"Hora (VET)", "Cómo se preparó"}, []float64{22, textW - 22}, []string{"B", "B"}, 8.5, "")
			color(pdfInk)
			for _, s := range ph.Prep {
				txt := s.Label
				if s.Later {
					txt += " (cambio posterior)"
				}
				row([]string{s.Time, txt}, []float64{22, textW - 22}, []string{"I", ""}, 9, "")
			}
		}
		pdf.Ln(1)
		color(pdfGraphite)
		pdf.SetFont("Lexend", "", 8)
		pdf.MultiCell(textW, 4, "Lo informó el teléfono con su propio reloj; no forma parte del sello.", "", "L", false)
	}

	for _, c := range r.Calls {
		heading(c.Title)
		facts(c.Facts)
		if len(c.Turns) > 0 {
			pdf.Ln(1)
			color(pdfGraphite)
			pdf.SetFont("Lexend", "B", 9)
			pdf.CellFormat(textW, 5, "Transcripción completa", "", 1, "L", false, 0, "")
			for _, t := range c.Turns {
				label := t.Speaker + " · " + t.Offset
				text := t.Text
				if t.Phrase {
					label += " · LA FRASE ACORDADA"
				}
				pdf.SetFont("Lexend", "", 9.5)
				n := len(pdf.SplitText(text, textW-8))
				if pdf.GetY()+float64(n)*4.8+8 > pageH-20 {
					pdf.AddPage()
				}
				if t.Phrase {
					fill(pdfLimeSoft)
				} else {
					fill(pdfPaper)
				}
				y := pdf.GetY()
				pdf.Rect(margin, y, textW, float64(n)*4.8+7, "F")
				pdf.SetXY(margin+3, y+1)
				color(pdfGraphite)
				pdf.SetFont("Lexend", "B", 8)
				pdf.CellFormat(textW-6, 4.4, label, "", 2, "L", false, 0, "")
				pdf.SetX(margin + 3)
				color(pdfInk)
				pdf.SetFont("Lexend", "", 9.5)
				pdf.MultiCell(textW-6, 4.8, text, "", "L", false)
				pdf.SetY(y + float64(n)*4.8 + 8)
			}
		}
	}

	for _, c := range r.Contexts {
		heading(c.Title)
		color(pdfGraphite)
		pdf.SetFont("Lexend", "", 8.5)
		pdf.MultiCell(textW, 4.2, ContextNote, "", "L", false)
		pdf.Ln(1.5)
		facts(c.Fields)
	}

	heading("Aviso a la familia")
	color(pdfInk)
	if len(r.Family) == 0 {
		pdf.SetFont("Lexend", "", 9.5)
		pdf.MultiCell(textW, 5, r.FamilyNote, "", "L", false)
	} else {
		color(pdfGraphite)
		ws := []float64{40, 34, 24, textW - 98}
		row([]string{"Familiar", "Estado", "Hora", "Detalle"}, ws, []string{"B", "B", "B", "B"}, 8.5, "")
		color(pdfInk)
		for _, f := range r.Family {
			row([]string{f.Recipient, f.Status, f.Time, f.Error}, ws, []string{"I", "", "", ""}, 9, "")
		}
		pdf.Ln(1)
		color(pdfGraphite)
		pdf.SetFont("Lexend", "", 8)
		pdf.MultiCell(textW, 4, "Estados según el proveedor de WhatsApp (Kapso) al generar este documento; no forman parte del sello.", "", "L", false)
	}
	if len(r.Emergencies) > 0 {
		heading("Emergencias sin conexión")
		color(pdfInk)
		pdf.SetFont("Lexend", "", 9.5)
		for _, e := range r.Emergencies {
			pdf.MultiCell(textW, 5, e, "", "L", false)
		}
		color(pdfGraphite)
		pdf.SetFont("Lexend", "", 8)
		pdf.MultiCell(textW, 4, "Del mismo teléfono y en la misma hora que la alerta: se enlazan por eso, no por un identificador. No forman parte del sello.", "", "L", false)
	}

	heading("El sello")
	color(pdfInk)
	if r.Draft {
		pdf.SetFont("Lexend", "", 9.5)
		pdf.MultiCell(textW, 5, "Sin sellar. "+r.SealNote, "", "L", false)
	} else {
		facts(r.Seal)
		pdf.Ln(1)
		for _, l := range r.Links {
			color(pdfGraphite)
			pdf.SetFont("Lexend", "", 9)
			pdf.CellFormat(52, 5, "Descarga "+l[0], "", 0, "L", false, 0, "")
			color(pdfOlive)
			pdf.SetFont("Lexend", "I", 9)
			pdf.CellFormat(textW-52, 5, l[1], "", 1, "L", false, 0, l[1])
		}
		pdf.Ln(2)
		color(pdfInk)
		pdf.SetFont("Lexend", "B", 9.5)
		pdf.CellFormat(textW, 5, "Cómo comprobarlo", "", 1, "L", false, 0, "")
		pdf.SetFont("Lexend", "", 9)
		for _, v := range r.Verify {
			pdf.MultiCell(textW, 4.8, v, "", "L", false)
		}
	}
	pdf.Ln(3)
	fill(pdfPaper)
	color(pdfInk)
	pdf.SetFont("Lexend", "I", 9.5)
	pdf.MultiCell(textW, 5, r.Honest, "", "L", true)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// pdf answers GET /i/{token}/reporte.pdf with the detailed report.
func (h *Handler) pdf(w http.ResponseWriter, r *http.Request) {
	rec, token, ok := h.load(w, r)
	if !ok {
		return
	}
	rep, err := buildPDF(rec, PDFOptions{Token: token, BaseURL: h.opts.BaseURL, Channel: h.opts.Channel, Now: h.now()})
	if err == nil {
		var b []byte
		if b, err = renderPDF(rep); err == nil {
			name := "ficha-mama-" + rec.Incident.TriggeredAt.In(vet).Format("2006-01-02-1504") + ".pdf"
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			_, _ = w.Write(b)
			return
		}
	}
	log.Printf("report: pdf: %v", err)
	http.Error(w, "No se pudo generar el PDF.", http.StatusInternalServerError)
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
