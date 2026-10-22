package hlc

import (
	"testing"

	pb "github.com/nawocci/kioku-sync/internal/pb"
)

func clock(wall int64, counter uint32, device string) *pb.Hlc {
	return &pb.Hlc{WallMs: wall, Counter: counter, Device: device}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b *pb.Hlc
		want int
	}{
		{"identical", clock(1, 1, "a"), clock(1, 1, "a"), 0},
		{"wall dominates", clock(2, 0, "a"), clock(1, 9, "z"), 1},
		{"counter breaks wall tie", clock(1, 2, "z"), clock(1, 1, "a"), 1},
		{"device breaks full tie", clock(1, 1, "b"), clock(1, 1, "a"), 1},
		{"reverse", clock(1, 1, "a"), clock(1, 1, "b"), -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Fatalf("Compare(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMax(t *testing.T) {
	a, b := clock(5, 0, "a"), clock(5, 1, "a")
	if got := Max(a, b); got != b {
		t.Fatalf("Max = %v, want %v", got, b)
	}
	if got := Max(b, a); got != b {
		t.Fatalf("Max reversed = %v, want %v", got, b)
	}
}

func TestClone(t *testing.T) {
	a := clock(5, 0, "a")
	c := Clone(a)
	c.WallMs = 9
	if a.WallMs != 5 {
		t.Fatal("Clone is not a deep copy")
	}
}
