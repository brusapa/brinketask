// Package webpush sends Web Push messages (D-15, D-63): it encrypts the
// payload for the browser's subscription keys (RFC 8291, "aes128gcm") and
// identifies the server to the push service with VAPID (RFC 8292). Only
// the Go standard library is used.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/brusapa/brinketask/internal/clock"
)

// b64 is base64url without padding, the encoding of every key in Web Push.
var b64 = base64.RawURLEncoding

// MaxPayload is the largest payload accepted: one 4096-byte record minus
// the 16-byte authentication tag and the padding delimiter (RFC 8291,
// section 4, allows 3993 bytes with the 86-byte header counted apart).
const MaxPayload = 3993

// uncompressedPointSize is the length of an uncompressed P-256 point:
// 0x04, then 32 bytes of x and 32 of y.
const uncompressedPointSize = 65

// recordSize is the "rs" of the aes128gcm header: one record holds it all.
const recordSize = 4096

// Keys is the server's VAPID key pair.
type Keys struct {
	private *ecdsa.PrivateKey
	// Public is the uncompressed P-256 point, base64url, as browsers want
	// it in pushManager.subscribe({applicationServerKey}).
	Public string
}

// GenerateKeys makes a new VAPID key pair and returns both halves encoded
// for configuration (D-68): the public point and the private scalar, in
// base64url.
func GenerateKeys() (public, private string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("webpush: generate keys: %w", err)
	}
	scalar, err := key.Bytes()
	if err != nil {
		return "", "", fmt.Errorf("webpush: encode private key: %w", err)
	}
	point, err := key.PublicKey.Bytes()
	if err != nil {
		return "", "", fmt.Errorf("webpush: encode public key: %w", err)
	}
	return b64.EncodeToString(point), b64.EncodeToString(scalar), nil
}

// ParseKeys reads a key pair as GenerateKeys writes it and checks that the
// two halves belong together.
func ParseKeys(public, private string) (Keys, error) {
	scalar, err := b64.DecodeString(private)
	if err != nil {
		return Keys{}, errors.New("webpush: private key is not base64url")
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	if err != nil {
		return Keys{}, fmt.Errorf("webpush: private key: %w", err)
	}
	point, err := key.PublicKey.Bytes()
	if err != nil {
		return Keys{}, fmt.Errorf("webpush: public key: %w", err)
	}
	if b64.EncodeToString(point) != public {
		return Keys{}, errors.New("webpush: the public key does not match the private key")
	}
	return Keys{private: key, Public: public}, nil
}

// Subscription is where and for whom a message is encrypted: the push
// service endpoint and the browser's keys, as PushSubscription.toJSON()
// gives them (base64url).
type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Encrypt encrypts a payload for a subscription (RFC 8291) and returns the
// request body: the aes128gcm header followed by the single record.
func Encrypt(sub Subscription, payload []byte) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	serverKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return encrypt(sub, payload, salt, serverKey)
}

// encrypt is Encrypt with the random parts given, so tests can reproduce
// the RFC 8291 example. The steps and names follow RFC 8291, section 3.4.
func encrypt(sub Subscription, payload, salt []byte, serverKey *ecdh.PrivateKey) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, fmt.Errorf("webpush: payload of %d bytes exceeds %d", len(payload), MaxPayload)
	}
	uaPublicBytes, err := b64.DecodeString(sub.P256dh)
	if err != nil {
		return nil, errors.New("webpush: p256dh is not base64url")
	}
	uaPublic, err := ecdh.P256().NewPublicKey(uaPublicBytes)
	if err != nil {
		return nil, fmt.Errorf("webpush: p256dh: %w", err)
	}
	authSecret, err := b64.DecodeString(sub.Auth)
	if err != nil || len(authSecret) != 16 {
		return nil, errors.New("webpush: auth is not 16 bytes of base64url")
	}

	ecdhSecret, err := serverKey.ECDH(uaPublic)
	if err != nil {
		return nil, err
	}
	asPublic := serverKey.PublicKey().Bytes()

	// key_info = "WebPush: info" || 0x00 || ua_public || as_public
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPublicBytes...), asPublic...)
	ikm, err := hkdf.Key(sha256.New, ecdhSecret, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// 0x02 marks the last (and only) record; no padding follows.
	plaintext := append(append([]byte{}, payload...), 0x02)

	// Header: salt (16) || rs (4, big endian) || idlen (1) || keyid.
	var body bytes.Buffer
	body.Write(salt)
	_ = binary.Write(&body, binary.BigEndian, uint32(recordSize))
	// keyid is the uncompressed P-256 point, always 65 bytes.
	body.WriteByte(uncompressedPointSize)
	body.Write(asPublic)
	body.Write(gcm.Seal(nil, nonce, plaintext, nil))
	return body.Bytes(), nil
}

// ErrGone means the subscription no longer exists at the push service
// (404 or 410); it must be disabled, not retried (SPEC section 6).
var ErrGone = errors.New("webpush: subscription gone")

// Sender posts messages to push services.
type Sender struct {
	keys    Keys
	subject string // VAPID "sub": a mailto: or https: URL
	client  *http.Client
	clock   clock.Clock
}

// NewSender returns a sender that signs with keys and identifies itself
// with subject.
func NewSender(keys Keys, subject string, client *http.Client, clk clock.Clock) *Sender {
	return &Sender{keys: keys, subject: subject, client: client, clock: clk}
}

// ttl is how long the push service keeps an undelivered message: a
// reminder older than a day has no use.
const ttl = 24 * time.Hour

// Send encrypts payload for sub and posts it. It returns ErrGone for a
// subscription that no longer exists, and another error for anything else
// that is not a success.
func (s *Sender) Send(ctx context.Context, sub Subscription, payload []byte) error {
	body, err := Encrypt(sub, payload)
	if err != nil {
		return err
	}
	authorization, err := s.authorization(sub.Endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webpush: request: %w", err)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(ttl.Seconds())))
	// "high" asks the push service to wake a sleeping device at once.
	req.Header.Set("Urgency", "high")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("webpush: post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// The body is read only to free the connection, never logged: it may
	// echo the request.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	default:
		return fmt.Errorf("webpush: push service answered %d", resp.StatusCode)
	}
}

// authorization builds the VAPID header (RFC 8292, section 3): a JWT signed
// with ES256 for the push service's origin, and the public key.
func (s *Sender) authorization(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("webpush: endpoint is not an absolute URL")
	}
	header, err := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		// At most 24 hours ahead (RFC 8292); 12 is common practice.
		"exp": s.clock.Now().Add(12 * time.Hour).Unix(),
		"sub": s.subject,
	})
	if err != nil {
		return "", err
	}
	signingInput := b64.EncodeToString(header) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	r, sig, err := ecdsa.Sign(rand.Reader, s.keys.private, digest[:])
	if err != nil {
		return "", fmt.Errorf("webpush: sign: %w", err)
	}
	// JWS ES256 signatures are r and s as two 32-byte big-endian numbers
	// (RFC 7518, section 3.4), not the ASN.1 form ecdsa.SignASN1 produces.
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	sig.FillBytes(signature[32:])
	return fmt.Sprintf("vapid t=%s.%s, k=%s", signingInput, b64.EncodeToString(signature), s.keys.Public), nil
}
