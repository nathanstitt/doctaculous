package standard

import "testing"

func TestLookup(t *testing.T) {
	tests := []struct {
		baseFont string
		wantName string
		wantOK   bool
	}{
		{"Helvetica", "TeXGyreHeros-Regular", true},
		{"Helvetica-Bold", "TeXGyreHeros-Regular", true},
		{"Helvetica-BoldOblique", "TeXGyreHeros-Regular", true},
		{"Arial", "TeXGyreHeros-Regular", true},
		{"ArialMT", "TeXGyreHeros-Regular", true},
		{"ABCDEF+Helvetica", "TeXGyreHeros-Regular", true},
		{"Times-Roman", "TeXGyreTermes-Regular", true},
		{"Times-BoldItalic", "TeXGyreTermes-Regular", true},
		{"TimesNewRoman", "TeXGyreTermes-Regular", true},
		{"TimesNewRomanPSMT", "TeXGyreTermes-Regular", true},
		{"Courier", "Inconsolata-Regular", true},
		{"Courier-Bold", "Inconsolata-Regular", true},
		{"CourierNew", "Inconsolata-Regular", true},
		{"Symbol", "", false},
		{"ZapfDingbats", "", false},
		{"SomeRandomFont", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.baseFont, func(t *testing.T) {
			sub, ok := Lookup(tc.baseFont)
			if ok != tc.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tc.baseFont, ok, tc.wantOK)
			}
			if ok {
				if sub.Name != tc.wantName {
					t.Errorf("Lookup(%q) name = %q, want %q", tc.baseFont, sub.Name, tc.wantName)
				}
				if len(sub.Data) == 0 {
					t.Errorf("Lookup(%q) returned empty Data", tc.baseFont)
				}
			}
		})
	}
}
