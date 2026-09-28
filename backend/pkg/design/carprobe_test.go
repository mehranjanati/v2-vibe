package design

import (
	"context"
	"testing"
)

func TestCarMarketplaceGetsListingBlock(t *testing.T) {
	b, err := Brief(context.Background(), "build simple car marketplace")
	if err != nil {
		t.Fatal(err)
	}
	if b.Product != "Automotive/Car Dealership" {
		t.Errorf("product=%q, want Automotive/Car Dealership", b.Product)
	}
	sel := SelectBlocksFor(b)
	t.Logf("blocks for car marketplace:")
	hasListing := false
	for _, blk := range sel {
		t.Logf("  %-16s section=%s", blk.Name, blk.Section)
		if blk.Name == "menu-grid" {
			hasListing = true
		}
	}
	if !hasListing {
		t.Error("car marketplace must include a product listing block (menu-grid)")
	}
}

func TestShadeHex(t *testing.T) {
	tests := []struct {
		hex  string
		amt  float64
		want string
	}{
		{"#0F172A", 0.08, "#22293b"},
		{"#F8FAFC", -0.25, "#babcbd"},
		{"#FFFFFF", -0.5, "#808080"},
		{"#000000", 0.5, "#7f7f7f"},
	}
	for _, tt := range tests {
		got := shadeHex(tt.hex, tt.amt)
		if got != tt.want {
			t.Errorf("shadeHex(%q, %v) = %q, want %q", tt.hex, tt.amt, got, tt.want)
		}
	}
}

func TestDarkPaletteElevated(t *testing.T) {
	b, _ := Brief(context.Background(), "build simple coffee landing")
	dark := b.darkPalette()
	// Dark bg should be dark (low luminance)
	if dark.Background == "#F8FAFC" {
		t.Error("dark background is still light")
	}
	// Card should be elevated (lighter than bg)
	if dark.Card == dark.Background {
		t.Error("dark card == background (no elevation)")
	}
	// Card should NOT be white (the old bug)
	if dark.Card == "#FFFFFF" || dark.Card == "#F8FAFC" {
		t.Errorf("dark card is light: %q", dark.Card)
	}
}
