package core

import (
	"testing"

	"github.com/go-rod/rod/lib/proto"
)

// A captcha solve is only worth its price if the proof of it outlives the
// request that paid for it. Google sets GOOGLE_ABUSE_EXEMPTION once a
// challenge is cleared; the engine hands that cookie to the lane store, and
// the next request on the same lane must get it back — otherwise every
// request buys its own solve.
func TestLaneCookiesCarryTheCaptchaExemptionToTheNextRequest(t *testing.T) {
	store := NewLaneStore(8)
	lane := ProxyLaneKey{Engine: "google", SessionID: "serpg1"}

	store.SaveCookies(lane, []*proto.NetworkCookie{
		{Name: "GOOGLE_ABUSE_EXEMPTION", Value: "ID=abc", Domain: ".google.com", Path: "/"},
	})

	restored := store.Cookies(lane)
	if len(restored) != 1 || restored[0].Name != "GOOGLE_ABUSE_EXEMPTION" {
		t.Fatalf("lane lost the exemption cookie: %+v", restored)
	}
	if restored[0].Value != "ID=abc" {
		t.Fatalf("exemption value = %q", restored[0].Value)
	}

	// A different sticky session is a different IP, so the exemption earned on
	// one lane must not be replayed on another — Google ties it to the client.
	other := ProxyLaneKey{Engine: "google", SessionID: "serpg2"}
	if got := store.Cookies(other); len(got) != 0 {
		t.Fatalf("exemption leaked to another lane: %+v", got)
	}
}

// SaveLaneCookies is the seam the Google engine uses after a solve. It must
// tolerate being called with nothing configured rather than panicking in the
// middle of a request that already succeeded.
func TestSaveLaneCookiesIsSafeWhenNothingIsConfigured(t *testing.T) {
	var browser *Browser
	browser.SaveLaneCookies(t.Context(), nil, "https://www.google.com/")

	configured := &Browser{}
	configured.SaveLaneCookies(t.Context(), nil, "https://www.google.com/")
}
