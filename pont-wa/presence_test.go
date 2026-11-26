package main

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// L'item émis : conv = user = l'id de la conversation (numéro), sans préfixe
// (l'interface ajoute « w: »).
func TestPresenceItem(t *testing.T) {
	conv := "15145551234@s.whatsapp.net"
	it := presenceItem(conv, &events.Presence{From: types.NewJID("15145551234", types.DefaultUserServer)})
	if it["conv"] != conv || it["user"] != conv || it["online"] != true || it["last_active"].(int64) == 0 {
		t.Errorf("en ligne : %v", it)
	}
	vu := time.UnixMilli(1764160000000)
	it = presenceItem(conv, &events.Presence{Unavailable: true, LastSeen: vu})
	if it["online"] != false || it["last_active"] != int64(1764160000000) {
		t.Errorf("hors ligne : %v", it)
	}
	it = presenceItem(conv, &events.Presence{Unavailable: true})
	if it["last_active"] != int64(0) {
		t.Errorf("heure cachée : %v", it)
	}
}

// Le compteur du journal : la première, puis un bilan par heure au plus.
func TestPresenceCompter(t *testing.T) {
	p := &Pont{}
	lid := types.NewJID("123456789", types.HiddenUserServer)
	p.presenceCompter(lid, false)
	if !p.pstats.premiere || p.pstats.recues != 1 || p.pstats.sansConv != 1 || p.pstats.lidSeul != 1 {
		t.Fatalf("%+v", p.pstats)
	}
	p.presenceCompter(types.NewJID("1514", types.DefaultUserServer), true)
	if p.pstats.recues != 2 || p.pstats.sansConv != 1 {
		t.Fatalf("%+v", p.pstats)
	}
	p.pstats.depuis = time.Now().Add(-2 * time.Hour)
	p.presenceCompter(lid, true)
	if p.pstats.recues != 0 {
		t.Errorf("bilan non remis à zéro : %+v", p.pstats)
	}
}
