package web

import "testing"

func TestNewTranslator_English(t *testing.T) {
	tr := NewTranslator("en")
	if got := tr("dashboard"); got != "Dashboard" {
		t.Fatalf("en 'dashboard' = %q, want 'Dashboard'", got)
	}
	if got := tr("save"); got != "Save" {
		t.Fatalf("en 'save' = %q, want 'Save'", got)
	}
}

func TestNewTranslator_Korean(t *testing.T) {
	tr := NewTranslator("ko")
	if got := tr("dashboard"); got != "대시보드" {
		t.Fatalf("ko 'dashboard' = %q, want '대시보드'", got)
	}
	if got := tr("save"); got != "저장" {
		t.Fatalf("ko 'save' = %q, want '저장'", got)
	}
}

func TestNewTranslator_Fallback(t *testing.T) {
	tr := NewTranslator("ja") // unsupported language
	// Should fall back to English.
	if got := tr("dashboard"); got != "Dashboard" {
		t.Fatalf("fallback 'dashboard' = %q, want 'Dashboard'", got)
	}
}

func TestNewTranslator_MissingKey(t *testing.T) {
	tr := NewTranslator("en")
	got := tr("nonexistent_key_xyz")
	if got != "nonexistent_key_xyz" {
		t.Fatalf("missing key should return key itself, got %q", got)
	}
}

func TestTranslationConsistency(t *testing.T) {
	en := translations["en"]
	ko := translations["ko"]

	var missing []string
	for key := range en {
		if _, ok := ko[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Errorf("keys present in 'en' but missing in 'ko' (%d):\n%v", len(missing), missing)
	}
}
