package report

import (
	"encoding/json"
	"strconv"
	"strings"
)

// CallContext is a call's context from #106's ficha (calls.ficha): what an
// LLM took from the transcript once the call ended. A field is "" when the
// call did not say ("no se sabe"). The JSON tags are its shape inside the
// sealed ficha.json (calls[].context, optional): field order is byte order.
type CallContext struct {
	Situacion string `json:"situacion,omitempty"` // one line: what seems to be happening
	Lugar     string `json:"lugar,omitempty"`
	Personas  string `json:"personas,omitempty"`
	Vehiculo  string `json:"vehiculo,omitempty"`
	Rumbo     string `json:"rumbo,omitempty"`
	Confianza string `json:"confianza,omitempty"` // baja, media or alta
}

// ContextNote says where the context comes from, wherever it is shown in
// full.
const ContextNote = "Lo extrajo automáticamente una IA a partir de la llamada: contrástalo con la grabación."

// parseCallContext reads a calls.ficha value, a JSON object or a string
// holding one, with its keys in any case and with or without accents. nil
// when it is empty, not an object, or says nothing.
func parseCallContext(raw json.RawMessage) *CallContext {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var c CallContext
	for k, v := range m {
		t := valueText(v)
		if unknownValue(t) {
			continue
		}
		switch plainKey(k) {
		case "situacion":
			c.Situacion = t
		case "lugar":
			c.Lugar = t
		case "personas":
			c.Personas = t
		case "vehiculo":
			c.Vehiculo = t
		case "rumbo":
			c.Rumbo = t
		case "confianza":
			c.Confianza = strings.ToLower(t)
		}
	}
	if c == (CallContext{}) {
		return nil
	}
	return &c
}

// Fields are the context's labelled fields that have a value, in reading
// order.
func (c CallContext) Fields() [][2]string {
	var out [][2]string
	for _, f := range [][2]string{
		{"Situación", c.Situacion}, {"Lugar", c.Lugar}, {"Personas", c.Personas},
		{"Vehículo", c.Vehiculo}, {"Rumbo", c.Rumbo}, {"Confianza", c.Confianza},
	} {
		if f[1] != "" {
			out = append(out, f)
		}
	}
	return out
}

// accents drops the Spanish accents from lowercase text.
var accents = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u")

// plainKey lowercases a key and drops its accents: "Vehículo" is vehiculo.
func plainKey(k string) string { return accents.Replace(strings.ToLower(strings.TrimSpace(k))) }

func valueText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "sí"
		}
		return "no"
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s := valueText(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func unknownValue(s string) bool {
	switch plainKey(s) {
	case "", "no se sabe", "desconocido", "n/a", "-", "—":
		return true
	}
	return false
}
