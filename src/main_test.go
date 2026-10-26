package main

import (
	"encoding/json"
	"testing"
)

func TestBuildVLESSConfigSupportsRealityAndVisionFlow(t *testing.T) {
	cfg, err := buildVLESSConfig("vless://11111111-1111-1111-1111-111111111111@example.com:443?type=tcp&security=reality&sni=example.com&fp=chrome&pbk=PUBLICKEY&sid=SHORTID&flow=xtls-rprx-vision", []string{"domain:example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(cfg, &got); err != nil {
		t.Fatal(err)
	}
	out := got["outbounds"].([]any)[0].(map[string]any)
	user := out["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
	if user["flow"] != "xtls-rprx-vision" {
		t.Fatalf("flow=%v", user["flow"])
	}
	stream := out["streamSettings"].(map[string]any)
	reality := stream["realitySettings"].(map[string]any)
	if reality["publicKey"] != "PUBLICKEY" || reality["shortId"] != "SHORTID" {
		t.Fatalf("reality=%v", reality)
	}
}

func TestBuildVLESSConfigRejectsUnsupportedTransport(t *testing.T) {
	if _, err := buildVLESSConfig("vless://11111111-1111-1111-1111-111111111111@example.com:443?type=quic&security=tls", nil); err == nil {
		t.Fatal("expected unsupported transport error")
	}
}
