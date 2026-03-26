package shortcode

import (
	"strings"
	"testing"
)

func TestGenerate_Length(t *testing.T) {
	for _, length := range []int{6, 7, 8, 10} {
		code, err := Generate(length)
		if err != nil {
			t.Fatalf("Generate(%d) error: %v", length, err)
		}
		if len(code) != length {
			t.Errorf("Generate(%d) = %q (len %d), want len %d", length, code, len(code), length)
		}
	}
}

func TestGenerate_Charset(t *testing.T) {
	for i := 0; i < 100; i++ {
		code, err := Generate(6)
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		for _, c := range code {
			if !strings.ContainsRune(Charset, c) {
				t.Errorf("Generate produced char %q not in charset", string(c))
			}
		}
	}
}

func TestGenerate_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 10000; i++ {
		code, err := Generate(6)
		if err != nil {
			t.Fatalf("Generate error: %v", err)
		}
		if seen[code] {
			t.Errorf("duplicate code %q at iteration %d", code, i)
		}
		seen[code] = true
	}
}

func TestGenerateUnique_NoCollision(t *testing.T) {
	code, err := GenerateUnique(6, func(code string) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("code length = %d, want 6", len(code))
	}
}

func TestGenerateUnique_CollisionRetry(t *testing.T) {
	calls := 0
	code, err := GenerateUnique(6, func(code string) (bool, error) {
		calls++
		return calls <= 2, nil // first 2 collide, 3rd succeeds
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("code length = %d, want 6", len(code))
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestGenerateUnique_LengthEscalation(t *testing.T) {
	calls := 0
	code, err := GenerateUnique(6, func(code string) (bool, error) {
		calls++
		return calls <= 3, nil // first 3 collide (all default-length), 4th succeeds
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(code) != 7 {
		t.Errorf("code length = %d, want 7 (escalated)", len(code))
	}
}

func TestGenerateUnique_Exhausted(t *testing.T) {
	_, err := GenerateUnique(6, func(code string) (bool, error) {
		return true, nil // always collides
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateCustomCode_Valid(t *testing.T) {
	cases := []string{"my-link", "abc", "a_b-c123", strings.Repeat("a", 32)}
	for _, code := range cases {
		if msg := ValidateCustomCode(code); msg != "" {
			t.Errorf("ValidateCustomCode(%q) = %q, want empty", code, msg)
		}
	}
}

func TestValidateCustomCode_TooShort(t *testing.T) {
	if msg := ValidateCustomCode("ab"); msg == "" {
		t.Error("expected error for 2-char code")
	}
}

func TestValidateCustomCode_TooLong(t *testing.T) {
	if msg := ValidateCustomCode(strings.Repeat("a", 33)); msg == "" {
		t.Error("expected error for 33-char code")
	}
}

func TestValidateCustomCode_InvalidChars(t *testing.T) {
	cases := []string{"my link", "my.link", "café", "a/b"}
	for _, code := range cases {
		if msg := ValidateCustomCode(code); msg == "" {
			t.Errorf("ValidateCustomCode(%q) = empty, want error", code)
		}
	}
}

func TestValidateCustomCode_Reserved(t *testing.T) {
	cases := []string{"api", "API", "health", "admin", "Health"}
	for _, code := range cases {
		msg := ValidateCustomCode(code)
		if msg != "code is reserved" {
			t.Errorf("ValidateCustomCode(%q) = %q, want 'code is reserved'", code, msg)
		}
	}
}
