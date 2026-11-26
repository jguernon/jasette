package main

// Tests de presence_flux.go, copié tel quel dans pont-meta et pont-ig.

import (
	"encoding/json"
	"testing"

	"go.mau.fi/mautrix-meta/pkg/instameow/thrift"
	"go.mau.fi/mautrix-meta/pkg/instameow/thrift/requeststream"
)

func TestParsePresence(t *testing.T) {
	b := []byte(`{"publishId":"x","publishType":2,"presenceUpdates":[
		{"userId":"17841400000000001","presenceStatus":2,"lastActiveTimeSeconds":1764160000},
		{"userId":17841400000000002,"presenceStatus":0,"lastActiveTimeSeconds":1764150000},
		{"userId":"17841400000000003","presenceStatus":null,"lastActiveTimeSeconds":null},
		{"userId":"0","presenceStatus":2},
		{"userId":"pas un id","presenceStatus":2}]}`)
	maj, complet, err := parsePresence(b)
	if err != nil {
		t.Fatal(err)
	}
	if complet {
		t.Error("publishType 2 : des changements, pas tout l'état")
	}
	want := []majPresence{
		{ID: 17841400000000001, Actif: true, Derniere: 1764160000000},
		{ID: 17841400000000002, Actif: false, Derniere: 1764150000000},
		{ID: 17841400000000003},
	}
	if len(maj) != len(want) {
		t.Fatalf("maj = %+v", maj)
	}
	for i := range want {
		if maj[i] != want[i] {
			t.Errorf("maj[%d] = %+v, voulu %+v", i, maj[i], want[i])
		}
	}
	if _, complet, _ := parsePresence([]byte(`{"publishType":1,"presenceUpdates":[]}`)); !complet {
		t.Error("publishType 1 : tout l'état")
	}
	if _, _, err := parsePresence([]byte(`pas du json`)); err == nil {
		t.Error("erreur attendue")
	}
}

func TestFluxOuvertureEtAmendement(t *testing.T) {
	params, corps := fluxOuverture("https://www.instagram.com/direct/inbox/", 3, "936619743392459")
	var p map[string]string
	if err := json.Unmarshal(params, &p); err != nil || p["x-dgw-app-XRSS-method"] != "PresenceUnifiedJSON" || p["x-dgw-app-XRSS-http_referer"] != "https://www.instagram.com/direct/inbox/" {
		t.Errorf("params = %s", params)
	}
	var c map[string]any
	if err := json.Unmarshal(corps, &c); err != nil || c["appFamily"] != 3.0 || c["appId"] != "936619743392459" || c["publishEncoding"] != 2.0 {
		t.Errorf("corps = %s", corps)
	}
	if _, ok := c["presenceReportingRequest"]; ok {
		t.Error("le pont ne doit pas annoncer sa propre présence")
	}
	if got := string(fluxAmendement([]int64{1, 22})); got != `{"payload":{"additionalContacts":{"additionalContacts":["1","22"]}}}` {
		t.Errorf("amendement = %s", got)
	}
}

func TestFluxTrame(t *testing.T) {
	var recu []majPresence
	f := &fluxPresence{nom: "Essai", recu: func(m []majPresence, complet bool) int {
		recu = append(recu, m...)
		return len(m)
	}}
	started := requeststream.FlowStatus_Started
	data := []byte(`{"publishType":2,"presenceUpdates":[{"userId":"5","presenceStatus":2}]}`)
	b, err := thrift.Marshal(&requeststream.Payload{Response: &requeststream.StreamResponse{Delta: []*requeststream.StreamResponseDelta{
		{FlowStatus: &started},
		{Data: &requeststream.ResponseData{Bytes: data}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	demarre, fatal := false, error(nil)
	f.trame(b, func(e error) { fatal = e }, func() { demarre = true })
	if !demarre || fatal != nil {
		t.Errorf("démarré %v, fatal %v", demarre, fatal)
	}
	if len(recu) != 1 || recu[0].ID != 5 || !recu[0].Actif {
		t.Errorf("reçu %+v", recu)
	}
	if !f.premier || f.nPub.Load() != 1 {
		t.Error("première présence non notée")
	}

	b, _ = thrift.Marshal(&requeststream.Payload{Response: &requeststream.StreamResponse{Delta: []*requeststream.StreamResponseDelta{
		{Termination: &requeststream.Termination{Reason: requeststream.TerminationReason_Rejected}},
	}}})
	f.trame(b, func(e error) { fatal = e }, func() {})
	if fatal == nil {
		t.Error("une fin de flux doit fermer le socket")
	}
}
