// Package webpushtest is a fake push service for tests. It does what a
// browser's push service and the browser together do: it checks the VAPID
// signature (RFC 8292) and decrypts the payload with the subscription's
// private keys (RFC 8291), so tests see exactly what a device would show.
package webpushtest

import (
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
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/brusapa/brinketask/internal/webpush"
)

var b64 = base64.RawURLEncoding

// Message is one push received: the decrypted payload and the device it
// was sent to.
type Message struct {
	Device  string // the path of the endpoint, which names the device
	Payload []byte
	TTL     string
}

// Service is a running fake push service.
type Service struct {
	server   *httptest.Server
	vapidKey string // the public key the server must sign with
	mu       sync.Mutex
	devices  map[string]*device
	messages []Message
	statuses map[string][]int // per device, the statuses to answer next
	failures []string
}

type device struct {
	key  *ecdh.PrivateKey
	auth []byte
}

// New starts a fake push service that accepts messages signed with the
// VAPID public key vapidPublic. It stops when the test ends.
func New(t *testing.T, vapidPublic string) *Service {
	t.Helper()
	s := &Service{vapidKey: vapidPublic, devices: map[string]*device{}, statuses: map[string][]int{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

// NewDevice registers a browser and returns its subscription, as
// PushSubscription.toJSON() would.
func (s *Service) NewDevice(t *testing.T, name string) webpush.Subscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.devices["/"+name] = &device{key: key, auth: auth}
	s.mu.Unlock()
	return webpush.Subscription{
		Endpoint: s.server.URL + "/" + name,
		P256dh:   b64.EncodeToString(key.PublicKey().Bytes()),
		Auth:     b64.EncodeToString(auth),
	}
}

// Answer makes the next requests to a device get these statuses, in order;
// afterwards it answers 201 again.
func (s *Service) Answer(name string, statuses ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses["/"+name] = append(s.statuses["/"+name], statuses...)
}

// Messages returns the messages received so far.
func (s *Service) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.messages...)
}

// Failures returns what was wrong with requests the service refused for a
// bad signature or encryption; a test fails if it is not empty.
func (s *Service) Failures() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.failures...)
}

func (s *Service) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	dev := s.devices[r.URL.Path]
	var status int
	if queue := s.statuses[r.URL.Path]; len(queue) > 0 {
		status, s.statuses[r.URL.Path] = queue[0], queue[1:]
	}
	s.mu.Unlock()

	if dev == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err == nil {
		err = s.checkVAPID(r, "http://"+r.Host)
	}
	var payload []byte
	if err == nil {
		payload, err = decrypt(dev, r.Header.Get("Content-Encoding"), body)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.failures = append(s.failures, err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.messages = append(s.messages, Message{Device: strings.TrimPrefix(r.URL.Path, "/"), Payload: payload, TTL: r.Header.Get("TTL")})
	w.WriteHeader(http.StatusCreated)
}

// checkVAPID verifies the Authorization header of RFC 8292.
func (s *Service) checkVAPID(r *http.Request, audience string) error {
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "vapid ")
	if !ok {
		return errors.New("no vapid authorization")
	}
	var token, key string
	for part := range strings.SplitSeq(value, ",") {
		name, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch name {
		case "t":
			token = v
		case "k":
			key = v
		}
	}
	if key != s.vapidKey {
		return fmt.Errorf("vapid key %q is not the server's", key)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("vapid token is not a JWT")
	}
	point, err := b64.DecodeString(key)
	if err != nil {
		return err
	}
	public, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
	if err != nil {
		return err
	}
	signature, err := b64.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		return errors.New("vapid signature is not 64 bytes")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	rr := new(big.Int).SetBytes(signature[:32])
	ss := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(public, digest[:], rr, ss) {
		return errors.New("vapid signature does not verify")
	}
	claimsJSON, err := b64.DecodeString(parts[1])
	if err != nil {
		return err
	}
	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return err
	}
	if claims.Aud != audience || claims.Sub == "" || claims.Exp == 0 {
		return fmt.Errorf("vapid claims %+v, want aud %s", claims, audience)
	}
	return nil
}

// decrypt is the receiving side of RFC 8291 (what the browser does).
func decrypt(dev *device, encoding string, body []byte) ([]byte, error) {
	if encoding != "aes128gcm" {
		return nil, fmt.Errorf("content encoding %q", encoding)
	}
	if len(body) < 21 {
		return nil, errors.New("body shorter than the header")
	}
	salt := body[:16]
	rs := binary.BigEndian.Uint32(body[16:20])
	idLen := int(body[20])
	if rs != 4096 || len(body) < 21+idLen {
		return nil, errors.New("bad header")
	}
	asPublicBytes := body[21 : 21+idLen]
	ciphertext := body[21+idLen:]
	asPublic, err := ecdh.P256().NewPublicKey(asPublicBytes)
	if err != nil {
		return nil, err
	}
	secret, err := dev.key.ECDH(asPublic)
	if err != nil {
		return nil, err
	}
	keyInfo := append(append([]byte("WebPush: info\x00"), dev.key.PublicKey().Bytes()...), asPublicBytes...)
	ikm, err := hkdf.Key(sha256.New, secret, dev.auth, string(keyInfo), 32)
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
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	if len(plain) == 0 || plain[len(plain)-1] != 0x02 {
		return nil, errors.New("missing last-record delimiter")
	}
	return plain[:len(plain)-1], nil
}
