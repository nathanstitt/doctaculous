package hevc

import "testing"

func TestChromaQPMapping(t *testing.T) {
	table := map[int32]int32{
		0: 0, 10: 10, 29: 29,
		30: 29, 31: 30, 32: 31, 33: 32, 34: 33, 35: 33, 36: 34,
		37: 34, 38: 35, 39: 35, 40: 36, 41: 36, 42: 37, 43: 37,
		44: 38, 45: 39, 51: 45,
	}
	for in, want := range table {
		if got := chromaQPMapping(in); got != want {
			t.Errorf("chromaQPMapping(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestDequantFlat(t *testing.T) {
	// qp 27, 8-bit, 4x4: bdShift = 5, scale = levelScale[3]<<4 = 912.
	// coeff 10 -> (10*16*912 + 16) >> 5 = 4560.
	block := make([]int32, 16)
	block[0] = 10
	block[5] = -3
	dequant(block, nil, 27, 2, 8)
	if block[0] != 4560 {
		t.Errorf("block[0] = %d, want 4560", block[0])
	}
	// (-3*16*912 + 16) >> 5 = -43760>>5 with rounding: (-43776+16)>>5 = -1367.5 -> -1368 (floor)
	if want := int32((-3*16*912 + 16) >> 5); block[5] != want {
		t.Errorf("block[5] = %d, want %d", block[5], want)
	}
	// Saturation at the 16-bit coefficient range.
	sat := make([]int32, 16)
	sat[0] = 32767
	dequant(sat, nil, 51, 2, 8)
	if sat[0] != 32767 {
		t.Errorf("saturated dequant = %d, want 32767", sat[0])
	}
	neg := make([]int32, 16)
	neg[0] = -32768
	dequant(neg, nil, 51, 2, 8)
	if neg[0] != -32768 {
		t.Errorf("saturated dequant = %d, want -32768", neg[0])
	}
	// Zero coefficients stay zero regardless of scaling.
	z := make([]int32, 16)
	dequant(z, nil, 51, 2, 10)
	for i, v := range z {
		if v != 0 {
			t.Fatalf("zero coeff %d became %d", i, v)
		}
	}
}

func TestDefaultScalingMatrices(t *testing.T) {
	sm, err := materializeScalingLists(nil)
	if err != nil {
		t.Fatalf("materialize defaults: %v", err)
	}
	for _, v := range sm.m[0][0] {
		if v != 16 {
			t.Fatalf("4x4 default not flat 16: %v", sm.m[0][0])
		}
	}
	m8 := sm.m[1][0] // 8x8 intra
	if m8[0] != 16 || m8[7] != 24 || m8[7*8+7] != 115 || m8[5*8+5] != 44 {
		t.Fatalf("8x8 intra default spot values wrong: %d %d %d %d", m8[0], m8[7], m8[63], m8[45])
	}
	if sm.m[1][3][7*8+7] != 91 {
		t.Fatalf("8x8 inter default corner = %d, want 91", sm.m[1][3][7*8+7])
	}
	// 16x16: 2x replication of the 8x8 grid with DC forced to 16.
	m16 := sm.m[2][0]
	if m16[0] != 16 {
		t.Errorf("16x16 DC = %d, want 16", m16[0])
	}
	if m16[15*16+15] != 115 || m16[1*16+1] != 16 || m16[15*16+1] != 24 {
		t.Errorf("16x16 replication wrong: %d %d %d", m16[15*16+15], m16[17], m16[15*16+1])
	}
	// 32x32 defines only matrix IDs 0 and 3.
	if sm.m[3][0] == nil || sm.m[3][3] == nil {
		t.Fatal("32x32 matrices missing")
	}
	if sm.m[3][1] != nil {
		t.Fatal("32x32 matrixID 1 should not be materialized")
	}
	if sm.m[3][0][31*32+31] != 115 {
		t.Errorf("32x32 corner = %d, want 115", sm.m[3][0][31*32+31])
	}
}

func TestExplicitScalingMatrix(t *testing.T) {
	sl := &scalingListData{}
	sl.predMode[0][0] = true
	for i := range 16 {
		sl.coeffs[0][0][i] = int32(i + 1) // 1..16 in diagonal scan order
	}
	sm, err := materializeScalingLists(sl)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	m := sm.m[0][0]
	// diag position 1 is (x 0, y 1), position 2 is (x 1, y 0).
	if m[0] != 1 || m[1*4+0] != 2 || m[0*4+1] != 3 || m[3*4+3] != 16 {
		t.Fatalf("diag->raster placement wrong: %v", m)
	}
	// Matrix 1 references matrix 0 via delta 1; matrix 2 falls back to the
	// default (delta 0).
	sl.refDelta[0][1] = 1
	sm, err = materializeScalingLists(sl)
	if err != nil {
		t.Fatalf("materialize with reference: %v", err)
	}
	for i := range 16 {
		if sm.m[0][1][i] != sm.m[0][0][i] {
			t.Fatalf("referenced matrix differs at %d", i)
		}
	}
	for _, v := range sm.m[0][2] {
		if v != 16 {
			t.Fatalf("default fallback not flat: %v", sm.m[0][2])
		}
	}
	// Explicit 32x32 with DC override.
	sl32 := &scalingListData{}
	sl32.predMode[3][0] = true
	for i := range 64 {
		sl32.coeffs[3][0][i] = 20
	}
	sl32.dcCoef[1][0] = 31
	sm, err = materializeScalingLists(sl32)
	if err != nil {
		t.Fatalf("materialize 32x32: %v", err)
	}
	if sm.m[3][0][0] != 31 {
		t.Errorf("32x32 DC = %d, want 31", sm.m[3][0][0])
	}
	if sm.m[3][0][5] != 20 {
		t.Errorf("32x32 body = %d, want 20", sm.m[3][0][5])
	}
}
