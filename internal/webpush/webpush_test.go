package webpush

import (
	"crypto/ecdh"
	"strings"
	"testing"
)

// unspace removes the whitespace the RFC puts inside long base64 values.
func unspace(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// RFC 8291, section 5 and appendix A: the example message, byte for byte.
func TestEncryptMatchesRFC8291Example(t *testing.T) {
	asPrivate, err := b64.DecodeString("yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw")
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdh.P256().NewPrivateKey(asPrivate)
	if err != nil {
		t.Fatal(err)
	}
	salt, _ := b64.DecodeString("DGv6ra1nlYgDCS1FRnbzlw")
	sub := Subscription{
		P256dh: unspace(`BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcx aOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4`),
		Auth:   "BTBZMqHH6r4Tts7J_aSIgg",
	}
	body, err := encrypt(sub, []byte("When I grow up, I want to be a watermelon"), salt, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	header := unspace(`DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z 9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml
		mlMoZIIgDll6e3vCYLocInmYWAmS6Tlz AC8wEqKK6PBru3jl7A8`)
	ciphertext := unspace(`8pfeW0KbunFT06SuDKoJH9Ql87S1QUrd irN6GcG7sFz1y1sqLgVi1VhjVkHsUoEs
		bI_0LpXMuGvnzQ`)
	wantHeader, _ := b64.DecodeString(header)
	wantCiphertext, _ := b64.DecodeString(ciphertext)
	want := append(wantHeader, wantCiphertext...)
	if b64.EncodeToString(body) != b64.EncodeToString(want) {
		t.Errorf("body =\n%s\nwant\n%s", b64.EncodeToString(body), b64.EncodeToString(want))
	}
}

func TestEncryptRejects(t *testing.T) {
	good := Subscription{P256dh: unspace(`BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcx aOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4`), Auth: "BTBZMqHH6r4Tts7J_aSIgg"}
	if _, err := Encrypt(good, make([]byte, MaxPayload+1)); err == nil {
		t.Error("an oversized payload was accepted")
	}
	bad := good
	bad.P256dh = "not a key"
	if _, err := Encrypt(bad, []byte("x")); err == nil {
		t.Error("a bad p256dh was accepted")
	}
	bad = good
	bad.Auth = "c2hvcnQ"
	if _, err := Encrypt(bad, []byte("x")); err == nil {
		t.Error("a short auth secret was accepted")
	}
}

// D-68: generated keys read back, and halves of different pairs do not.
func TestKeys(t *testing.T) {
	public, private, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ParseKeys(public, private)
	if err != nil {
		t.Fatal(err)
	}
	if keys.Public != public || len(public) != 87 || len(private) != 43 {
		t.Errorf("public %q (%d), private length %d", public, len(public), len(private))
	}
	otherPublic, _, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseKeys(otherPublic, private); err == nil {
		t.Error("mismatched keys accepted")
	}
	if _, err := ParseKeys(public, "!!"); err == nil {
		t.Error("garbage accepted")
	}
}
