package secrets

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	box, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.Seal([]byte("sk-test"))
	if bytes.Contains(sealed, []byte("sk-test")) {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := box.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "sk-test" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenRejectsTamperedOrWrongKey(t *testing.T) {
	box, _ := New(bytes.Repeat([]byte{1}, 32))
	other, _ := New(bytes.Repeat([]byte{2}, 32))
	sealed := box.Seal([]byte("secret"))

	if _, err := other.Open(sealed); err == nil {
		t.Error("wrong key: want error")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := box.Open(sealed); err == nil {
		t.Error("tampered: want error")
	}
	if _, err := box.Open([]byte{1, 2}); err == nil {
		t.Error("short: want error")
	}
}
