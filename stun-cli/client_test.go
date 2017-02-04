package main

import (
	"log"
	"os"
	"strings"
	"testing"

	"github.com/ernado/stun"
)

const (
	envExternalBlackbox = "TEST_EXTERNAL"
)

func isFlagged(env string) bool {
	switch strings.ToUpper(os.Getenv(env)) {
	case "YES", "Y", "1", "TRUE", "ПОЧЕМУ БЫ И НЕТ?", "IF YOU INSIST":
		return true
	default:
		return false
	}
}

func skipIfNotFlagged(t *testing.T, env string) {
	if !isFlagged(env) {
		t.Skipf("Test disabled by absent environment variable %s", env)
	}
}
func TestClient_Do(t *testing.T) {
	skipIfNotFlagged(t, envExternalBlackbox)
	client := Client{}
	m := stun.New()
	m.Type = stun.MessageType{Method: stun.MethodBinding, Class: stun.ClassRequest}
	m.TransactionID = stun.NewTransactionID()
	m.Add(stun.NewSoftware("cydev/stun alpha"))
	m.WriteHeader()
	request := Request{
		Target:  "stun.l.google.com:19302",
		Message: m,
	}
	if err := client.Do(request, func(r Response) error {
		if r.Message.TransactionID != m.TransactionID {
			t.Error("transaction id messmatch")
		}
		ip, port, err := r.Message.GetXORMappedAddress()
		if err != nil {
			t.Error(err)
		}
		log.Println("got", ip, port)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
