// Command verify-ficha checks the seal of an incident report (#76): that
// ficha.sig is the MAMÁ server's Ed25519 signature over ficha.json, byte for
// byte. Take the public key from the report page or from the server's GET
// /config (server_ed25519_public_key), not from ficha.json itself.
//
//	cd server
//	go run ./cmd/verify-ficha ficha.json ficha.sig <public key, std Base64>
//
// It prints "firma válida" and exits 0, or says why the seal fails and
// exits 1 (2 for bad arguments). A valid seal proves the record did not
// change since the MAMÁ server closed it; it is not a certified electronic
// signature and does not replace a forensic examination. The format is in
// docs/adr/0005-incident-report-seal.md.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/report"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 3 {
		fmt.Fprintln(stderr, "uso: go run ./cmd/verify-ficha ficha.json ficha.sig <llave pública en Base64>")
		return 2
	}
	doc, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "no se pudo leer la ficha: %v\n", err)
		return 2
	}
	rawSig, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintf(stderr, "no se pudo leer la firma: %v\n", err)
		return 2
	}
	pub, err := report.ParsePublicKey(args[2])
	if err != nil {
		fmt.Fprintf(stdout, "firma NO válida: %v\n", err)
		return 1
	}
	sig, err := report.ParseSignature(rawSig)
	if err != nil {
		fmt.Fprintf(stdout, "firma NO válida: %v\n", err)
		return 1
	}
	sealed, err := report.Verify(doc, sig, pub)
	if err != nil {
		fmt.Fprintf(stdout, "firma NO válida: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "firma válida")
	fmt.Fprintf(stdout, "incidente %s, sellado %s, %d llamada(s)\n", sealed.IncidentID, sealed.SealedAt, sealed.Calls)
	fmt.Fprintf(stdout, "llave %s\n", report.Fingerprint(pub))
	fmt.Fprintln(stdout, "El sello prueba que el registro no cambió desde que el servidor de MAMÁ lo cerró. No es una firma electrónica certificada ni reemplaza una experticia.")
	return 0
}
