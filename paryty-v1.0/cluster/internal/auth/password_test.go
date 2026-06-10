package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_Success(t *testing.T) {
	hash, err := HashPassword("ValidP@ss1")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("hash must not be empty")
	}
	// bcrypt hash starts with $2a$
	if !strings.HasPrefix(hash, "$2a$") {
		t.Errorf("expected bcrypt hash prefix $2a$, got %q", hash[:5])
	}
}

func TestHashPassword_Empty(t *testing.T) {
	hash, err := HashPassword("")
	if err != nil {
		t.Fatalf("HashPassword should not error on empty string: %v", err)
	}
	if hash == "" {
		t.Fatal("empty password should still produce a hash")
	}
}

func TestHashPassword_AboveMaxLength(t *testing.T) {
	longPass := strings.Repeat("a", MaxPasswordLength+1)
	_, err := HashPassword(longPass)
	if err == nil {
		t.Fatal("expected error for password exceeding max length")
	}
}

func TestHashPassword_ExactlyMaxLength(t *testing.T) {
	longPass := strings.Repeat("a", MaxPasswordLength)
	hash, err := HashPassword(longPass)
	if err != nil {
		t.Fatalf("HashPassword should accept max-length password: %v", err)
	}
	if hash == "" {
		t.Fatal("hash must not be empty")
	}
}

func TestVerifyPassword_Correct(t *testing.T) {
	password := "CorrectP@ss1"
	hash, _ := HashPassword(password)

	err := VerifyPassword(password, hash)
	if err != nil {
		t.Errorf("expected match, got error: %v", err)
	}
}

func TestVerifyPassword_Wrong(t *testing.T) {
	hash, _ := HashPassword("CorrectP@ss1")

	err := VerifyPassword("WrongPassword", hash)
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestVerifyPassword_InvalidHash(t *testing.T) {
	err := VerifyPassword("anything", "not-a-valid-bcrypt-hash")
	if err == nil {
		t.Fatal("expected error for invalid hash")
	}
}

func TestHashPassword_DeterministicSalt(t *testing.T) {
	// bcrypt should generate different hashes for the same password
	// because each hash includes a random salt.
	h1, _ := HashPassword("SamePassword1!")
	h2, _ := HashPassword("SamePassword1!")
	if h1 == h2 {
		t.Error("bcrypt hashes of same password should differ due to random salt")
	}
	// Both should verify correctly
	if err := VerifyPassword("SamePassword1!", h1); err != nil {
		t.Errorf("h1 should verify: %v", err)
	}
	if err := VerifyPassword("SamePassword1!", h2); err != nil {
		t.Errorf("h2 should verify: %v", err)
	}
}

func TestValidatePasswordPolicy_Valid(t *testing.T) {
	tests := []string{
		"ValidP@ss1",
		"C0mpl3x!Pass",
		"Aa1!aaaa",
		"My_S3cur3_P@$$w0rd",
	}
	for _, pw := range tests {
		t.Run(pw, func(t *testing.T) {
			if err := ValidatePasswordPolicy(pw); err != nil {
				t.Errorf("expected valid: %v", err)
			}
		})
	}
}

func TestValidatePasswordPolicy_TooShort(t *testing.T) {
	err := ValidatePasswordPolicy("Abc1!")
	if err == nil {
		t.Fatal("expected error for short password")
	}
	if !strings.Contains(err.Error(), "at least 8") {
		t.Errorf("expected length error, got: %v", err)
	}
}

func TestValidatePasswordPolicy_TooLong(t *testing.T) {
	long := strings.Repeat("Aa1!aaaa", 20) // 160 chars
	err := ValidatePasswordPolicy(long)
	if err == nil {
		t.Fatal("expected error for long password")
	}
	if !strings.Contains(err.Error(), "at most 72") {
		t.Errorf("expected max length error, got: %v", err)
	}
}

func TestValidatePasswordPolicy_MissingUppercase(t *testing.T) {
	err := ValidatePasswordPolicy("alllowercase1!")
	if err == nil {
		t.Fatal("expected error for missing uppercase")
	}
	if !strings.Contains(err.Error(), "uppercase") {
		t.Errorf("expected uppercase error, got: %v", err)
	}
}

func TestValidatePasswordPolicy_MissingLowercase(t *testing.T) {
	err := ValidatePasswordPolicy("ALLUPPERCASE1!")
	if err == nil {
		t.Fatal("expected error for missing lowercase")
	}
	if !strings.Contains(err.Error(), "lowercase") {
		t.Errorf("expected lowercase error, got: %v", err)
	}
}

func TestValidatePasswordPolicy_MissingDigit(t *testing.T) {
	err := ValidatePasswordPolicy("NoDigitsHere!")
	if err == nil {
		t.Fatal("expected error for missing digit")
	}
	if !strings.Contains(err.Error(), "digit") {
		t.Errorf("expected digit error, got: %v", err)
	}
}

func TestValidatePasswordPolicy_MissingSpecial(t *testing.T) {
	err := ValidatePasswordPolicy("NoSpecial1")
	if err == nil {
		t.Fatal("expected error for missing special character")
	}
	if !strings.Contains(err.Error(), "special") {
		t.Errorf("expected special character error, got: %v", err)
	}
}

func TestValidatePasswordPolicy_ExactlyMinLength(t *testing.T) {
	// 8 chars: requires at least one upper, lower, digit, special
	if err := ValidatePasswordPolicy("A1!bcdef"); err != nil {
		t.Errorf("8-char password meeting all criteria should be valid: %v", err)
	}
}

func TestValidatePasswordPolicy_UnicodeSpecial(t *testing.T) {
	// Test with various special characters
	tests := []string{
		"Pass1@word",   // @ sign
		"Pass1#word",   // # sign
		"Pass1$word",   // $ sign
		"Pass1%word",   // % sign
		"Pass1^word",   // ^ sign
		"Pass1&word",   // & sign
		"Pass1*word",   // * sign
		"Pass1_word",   // underscore
	}
	for _, pw := range tests {
		t.Run(pw, func(t *testing.T) {
			if err := ValidatePasswordPolicy(pw); err != nil {
				t.Errorf("%q should be valid: %v", pw, err)
			}
		})
	}
}
