package vmnet

import (
	"os"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/nuget"
)

// TestCertifiedNuGetPackages downloads real, published NuGet packages live
// and calls a real function from each — proof that vmnet executes
// third-party .NET code, not just its own test fixtures (spec §36).
//
// It hits the network, so it's opt-in: set VMNET_NETWORK_TESTS=1. See
// docs/en/ROADMAP.md Fase 3 for the full certification methodology (which
// packages were tried, what `vmnet check package` said about each, and
// why these three specific functions were picked).
func TestCertifiedNuGetPackages(t *testing.T) {
	if os.Getenv("VMNET_NETWORK_TESTS") == "" {
		t.Skip("set VMNET_NETWORK_TESTS=1 to run (downloads real packages from nuget.org)")
	}

	t.Run("Newtonsoft.Json/MathUtils.ApproxEquals", func(t *testing.T) {
		asm := loadCertifiedPackage(t, "Newtonsoft.Json", "13.0.3")
		tests := []struct {
			a, b float64
			want bool
		}{
			{1.0, 1.0, true},
			{1.0, 1.0000000000000002, true}, // within float epsilon
			{1.0, 1.1, false},
			{100.0, 100.00000000000003, true},
		}
		for _, tt := range tests {
			out, err := asm.Call("Newtonsoft.Json.Utilities.MathUtils", "ApproxEquals", Float64(tt.a), Float64(tt.b))
			if err != nil {
				t.Fatalf("ApproxEquals(%v, %v): %v", tt.a, tt.b, err)
			}
			if got := out.Native().(int32) != 0; got != tt.want {
				t.Errorf("ApproxEquals(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		}
	})

	t.Run("System.Text.Json/HexConverter.IsHexUpperChar", func(t *testing.T) {
		asm := loadCertifiedPackage(t, "System.Text.Json", "8.0.5")
		tests := []struct {
			c    rune
			want bool
		}{
			{'0', true}, {'9', true}, {'A', true}, {'F', true},
			{'a', false}, {'f', false}, {'G', false},
			// ' ' (0x20) regression-tests the signed/unsigned comparison
			// fix: '0'-48 underflows to a huge value under ble.un, and this
			// exact call site is what first caught the bug (see
			// docs/en/ROADMAP.md Fase 3).
			{' ', false},
		}
		for _, tt := range tests {
			out, err := asm.Call("System.HexConverter", "IsHexUpperChar", Int32(int32(tt.c)))
			if err != nil {
				t.Fatalf("IsHexUpperChar(%q): %v", tt.c, err)
			}
			if got := out.Native().(int32) != 0; got != tt.want {
				t.Errorf("IsHexUpperChar(%q) = %v, want %v", tt.c, got, tt.want)
			}
		}
	})

	t.Run("SimpleBase/Base32.getAllocationByteCountForDecoding", func(t *testing.T) {
		asm := loadCertifiedPackage(t, "SimpleBase", "4.0.0")
		tests := []struct{ in, want int32 }{
			{0, 0}, {8, 5}, {16, 10}, {100, 62}, {1000, 625},
		}
		for _, tt := range tests {
			out, err := asm.Call("SimpleBase.Base32", "getAllocationByteCountForDecoding", Int32(tt.in))
			if err != nil {
				t.Fatalf("getAllocationByteCountForDecoding(%d): %v", tt.in, err)
			}
			if got := out.Native().(int32); got != tt.want {
				t.Errorf("getAllocationByteCountForDecoding(%d) = %d, want %d", tt.in, got, tt.want)
			}
		}
	})
}

func loadCertifiedPackage(t *testing.T, id, version string) *Assembly {
	t.Helper()
	data, err := nuget.NewClient().Download(id, version)
	if err != nil {
		t.Skipf("network unavailable or %s@%s moved: %v", id, version, err)
	}
	pkg, err := nuget.OpenPackage(data)
	if err != nil {
		t.Fatalf("opening %s@%s: %v", id, version, err)
	}
	asset, ok, reason := pkg.SelectLibAsset(nuget.SelectOptions{})
	if !ok {
		t.Fatalf("%s@%s: no selectable asset: %s", id, version, reason)
	}
	asm, err := New().LoadBytes(id, asset.Data)
	if err != nil {
		t.Fatalf("loading %s@%s: %v", id, version, err)
	}
	return asm
}
