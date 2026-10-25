package report

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// The phone's permissions (#125): the "Teléfono listo" node at the head of
// the track (capa 1, from the alert's device_state) and the "Preparación del
// teléfono" strip on the central detail and in the PDF (capa 2, from the
// device's permission events). Neither is part of the seal: ficha.json v1
// does not carry them, and the PDF says so.

// permissionText is how the page names a permission, in plain words: On when
// granted, Off when not. Important ones turn the node yellow when missing.
type permissionText struct {
	On, Off   string
	Important bool
}

// permissionOrder is setup's order (#15), the order the chips and the strip
// list permissions in. It mirrors the app's PermissionKeys.ALL.
var permissionOrder = []string{
	contract.PermissionNotifications,
	contract.PermissionLocationFine,
	contract.PermissionLocationBackground,
	contract.PermissionMicrophone,
	contract.PermissionContacts,
	contract.PermissionSMS,
	contract.PermissionBatteryUnrestricted,
	contract.PermissionFullScreenIntent,
	contract.PermissionHibernationExempt,
	contract.PermissionAccessibilityService,
}

var permissionTexts = map[string]permissionText{
	contract.PermissionNotifications:        {"Notificaciones", "Notificaciones apagadas", true},
	contract.PermissionLocationFine:         {"Ubicación precisa", "Ubicación aproximada", true},
	contract.PermissionLocationBackground:   {"Ubicación siempre", "Ubicación solo con la app abierta", true},
	contract.PermissionMicrophone:           {"Micrófono", "Sin micrófono", false},
	contract.PermissionContacts:             {"Contactos", "Sin contactos", false},
	contract.PermissionSMS:                  {"Mensajes SMS", "Sin mensajes SMS", false},
	contract.PermissionBatteryUnrestricted:  {"Batería sin restricciones", "Batería con restricciones", true},
	contract.PermissionFullScreenIntent:     {"Avisos a pantalla completa", "Sin avisos a pantalla completa", false},
	contract.PermissionHibernationExempt:    {"Activa aunque no se use", "Se pausa si no se usa", false},
	contract.PermissionAccessibilityService: {"Gesto de volumen", "Gesto de volumen apagado", true},
}

// permissionRank is a permission's place in permissionOrder; names the page
// does not know go last.
func permissionRank(name string) int {
	for i, n := range permissionOrder {
		if n == name {
			return i
		}
	}
	return len(permissionOrder)
}

// PhoneReady is the "Teléfono listo" node's content: the phone's
// permissions when the person asked for help.
type PhoneReady struct {
	Summary string // "7 de 8 ✓": granted of those that apply to the phone
	Warn    bool   // an important permission is missing: the node is yellow
	Missing string // the important ones missing, for the screen reader and the PDF
	Time    string // when the phone read them (phone clock)
	Chips   []PermissionChip
}

// PermissionChip is one permission in the node's list.
type PermissionChip struct {
	Key       string
	Name      string // a contract.Permission* name
	Label     string // the On or Off text
	OK        bool
	Important bool
}

// phoneReady builds the node from the alert's snapshot; nil without one.
// Only the permissions the page can name count: a newer app's are skipped.
func phoneReady(rec Record) *PhoneReady {
	s := rec.DeviceState
	if s == nil {
		return nil
	}
	r := &PhoneReady{}
	granted := 0
	var missing []string
	for _, name := range permissionOrder {
		ok, applies := s.Permissions[name]
		if !applies {
			continue
		}
		t := permissionTexts[name]
		c := PermissionChip{Key: "perm-" + name + "-" + strconv.FormatBool(ok), Name: name, Label: t.Off, OK: ok, Important: t.Important}
		if ok {
			c.Label = t.On
			granted++
		} else if t.Important {
			r.Warn = true
			missing = append(missing, t.Off)
		}
		r.Chips = append(r.Chips, c)
	}
	if len(r.Chips) == 0 {
		return nil
	}
	r.Summary = fmt.Sprintf("%d de %d ✓", granted, len(r.Chips))
	for i, m := range missing {
		if i > 0 {
			r.Missing += ", "
		}
		r.Missing += m
	}
	at := s.CapturedAt
	if at.IsZero() {
		at = rec.Incident.TriggeredAt
	}
	r.Time = clock(at)
	return r
}

// PrepStep is one entry of the "Preparación del teléfono" strip.
type PrepStep struct {
	Key   string
	Label string // the On text for a grant, the Off text for a loss
	Time  string // phone clock, "10:02"
	Later bool   // not the permission's first grant: a change after setup
	Lost  bool   // the permission was taken away
}

// prepUntil is how long after the gesture a permission event still counts
// for the incident: the trigger syncs the changes right after the alert. A
// later change describes the phone after the case, not during it.
const prepUntil = 10 * time.Minute

// prepSteps builds the strip: each permission's first grant, in time order
// (setup's order breaks ties), then every later change in time order,
// marked. Events after the gesture plus prepUntil are left out. Nil when the
// device sent none.
func prepSteps(rec Record) []PrepStep {
	until := rec.Incident.TriggeredAt.Add(prepUntil)
	events := make([]PermissionEvent, 0, len(rec.PermissionEvents))
	for _, e := range rec.PermissionEvents {
		if _, known := permissionTexts[e.Permission]; known && !e.At.After(until) {
			events = append(events, e)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].At.Equal(events[j].At) {
			return events[i].At.Before(events[j].At)
		}
		return permissionRank(events[i].Permission) < permissionRank(events[j].Permission)
	})
	var setup, later []PrepStep
	granted := map[string]bool{}
	for i, e := range events {
		t := permissionTexts[e.Permission]
		s := PrepStep{Key: "prep-" + strconv.Itoa(i) + "-" + e.Permission, Time: e.At.In(vet).Format("15:04"), Label: t.On}
		switch {
		case e.Granted && !granted[e.Permission]:
			granted[e.Permission] = true
			setup = append(setup, s)
			continue
		case !e.Granted:
			s.Label, s.Lost = t.Off, true
		}
		s.Later = true
		later = append(later, s)
	}
	return append(setup, later...)
}
