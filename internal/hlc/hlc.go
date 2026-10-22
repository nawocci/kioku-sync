// Package hlc orders hybrid logical clock values: wall time first, then the
// logical counter, then the device id as a deterministic tiebreak.
package hlc

import (
	"cmp"

	pb "github.com/nawocci/kioku-sync/internal/pb"

	"google.golang.org/protobuf/proto"
)

// Compare orders two clock values. It returns -1 when a happened before b,
// 0 when they are identical, and 1 otherwise.
func Compare(a, b *pb.Hlc) int {
	if c := cmp.Compare(a.GetWallMs(), b.GetWallMs()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.GetCounter(), b.GetCounter()); c != 0 {
		return c
	}
	return cmp.Compare(a.GetDevice(), b.GetDevice())
}

// Max returns whichever of the two values happened last.
func Max(a, b *pb.Hlc) *pb.Hlc {
	if Compare(a, b) >= 0 {
		return a
	}
	return b
}

// Clone deep-copies a clock value.
func Clone(v *pb.Hlc) *pb.Hlc {
	return proto.Clone(v).(*pb.Hlc)
}
