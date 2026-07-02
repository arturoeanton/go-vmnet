package metadata

import "testing"

// FuzzParseSignatures proves the four signature-blob parsers (method/field/
// local-var/typespec — all reachable from raw, attacker-controlled bytes
// inside a DLL's #Blob stream) can't be made to panic, hang or read out of
// bounds on malformed input. They were previously exercised only
// indirectly (FuzzParse never actually calls them — Field/MethodDef rows
// hand back raw signature bytes without parsing them), so this closes a
// real gap in Fase 3.5's ParseFieldSig addition and the pre-existing
// method/local-var/typespec parsers alike.
func FuzzParseSignatures(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x06})                   // FIELD marker, nothing else
	f.Add([]byte{0x07})                   // LOCAL_SIG marker, nothing else
	f.Add([]byte{0x06, 0x08})             // field: I4
	f.Add([]byte{0x00, 0x00, 0x08})       // method: 0 params, ret I4
	f.Add([]byte{0x00, 0x01, 0x08, 0x08}) // method: 1 param I4, ret I4
	f.Add([]byte{0x20, 0x00, 0x1C})       // method: HASTHIS, 0 params, ret object
	f.Add([]byte{0x07, 0x01, 0x08})       // local sig: 1 local, I4
	f.Add([]byte{0x1D, 0x08})             // typespec: SZARRAY of I4
	f.Add([]byte{0x1F, 0x00, 0x08})       // CMOD_REQD with truncated token, then I4
	f.Add(make([]byte, 32))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseFieldSig(data)
		_, _ = ParseMethodSig(data)
		_, _ = ParseLocalVarSig(data)
		_, _ = ParseTypeSpec(data)
	})
}
