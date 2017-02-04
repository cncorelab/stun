package stun

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"net"
	"strings"
	"testing"
)

func TestMessage_AddSoftware(t *testing.T) {
	m := New()
	v := "Client v0.0.1"
	m.AddRaw(AttrSoftware, []byte(v))
	m.WriteHeader()

	m2 := New()
	if _, err := m2.ReadFrom(m.reader()); err != nil {
		t.Error(err)
	}
	vRead := m.GetSoftware()
	if vRead != v {
		t.Errorf("Expected %s, got %s.", v, vRead)
	}

	sAttr, ok := m.Attributes.Get(AttrSoftware)
	if !ok {
		t.Error("sowfware attribute should be found")
	}
	s := sAttr.String()
	if !strings.HasPrefix(s, "SOFTWARE:") {
		t.Error("bad string representation", s)
	}
}

func TestMessage_GetSoftware(t *testing.T) {
	m := New()
	v := m.GetSoftware()
	if v != "" {
		t.Errorf("%s should be blank.", v)
	}
	vByte := m.GetSoftwareBytes()
	if vByte != nil {
		t.Errorf("%s should be nil.", vByte)
	}
}

func BenchmarkMessage_AddXORMappedAddress(b *testing.B) {
	m := New()
	b.ReportAllocs()
	ip := net.ParseIP("192.168.1.32")
	for i := 0; i < b.N; i++ {
		m.AddXORMappedAddress(ip, 3654)
		m.Reset()
	}
}

func BenchmarkMessage_GetXORMappedAddress(b *testing.B) {
	m := New()
	transactionID, err := base64.StdEncoding.DecodeString("jxhBARZwX+rsC6er")
	if err != nil {
		b.Error(err)
	}
	copy(m.TransactionID[:], transactionID)
	addrValue, err := hex.DecodeString("00019cd5f49f38ae")
	if err != nil {
		b.Error(err)
	}
	for i := 0; i < b.N; i++ {
		m.AddRaw(AttrXORMappedAddress, addrValue)
		m.GetXORMappedAddress()
		m.Reset()
	}
}

func TestMessage_GetXORMappedAddress(t *testing.T) {
	m := New()
	transactionID, err := base64.StdEncoding.DecodeString("jxhBARZwX+rsC6er")
	if err != nil {
		t.Error(err)
	}
	copy(m.TransactionID[:], transactionID)
	addrValue, err := hex.DecodeString("00019cd5f49f38ae")
	if err != nil {
		t.Error(err)
	}
	m.AddRaw(AttrXORMappedAddress, addrValue)
	ip, port, err := m.GetXORMappedAddress()
	if err != nil {
		t.Error(err)
	}
	if !ip.Equal(net.ParseIP("213.141.156.236")) {
		t.Error("bad ip", ip, "!=", "213.141.156.236")
	}
	if port != 48583 {
		t.Error("bad port", port, "!=", 48583)
	}
}

func TestMessage_GetXORMappedAddressBad(t *testing.T) {
	m := New()
	transactionID, err := base64.StdEncoding.DecodeString("jxhBARZwX+rsC6er")
	if err != nil {
		t.Error(err)
	}
	copy(m.TransactionID[:], transactionID)
	expectedIP := net.ParseIP("213.141.156.236")
	expectedPort := 21254

	_, _, err = m.GetXORMappedAddress()
	if err == nil {
		t.Fatal(err, "should be nil")
	}

	m.AddXORMappedAddress(expectedIP, expectedPort)
	m.WriteHeader()

	mRes := New()
	binary.BigEndian.PutUint16(m.Raw[20+4:20+4+2], 0x21)
	if _, err = mRes.ReadFrom(bytes.NewReader(m.Raw)); err != nil {
		t.Fatal(err)
	}
	_, _, err = m.GetXORMappedAddress()
	if err == nil {
		t.Fatal(err, "should not be nil")
	}
}

func TestMessage_AddXORMappedAddress(t *testing.T) {
	m := New()
	transactionID, err := base64.StdEncoding.DecodeString("jxhBARZwX+rsC6er")
	if err != nil {
		t.Error(err)
	}
	copy(m.TransactionID[:], transactionID)
	expectedIP := net.ParseIP("213.141.156.236")
	expectedPort := 21254
	m.AddXORMappedAddress(expectedIP, expectedPort)
	m.WriteHeader()

	mRes := New()
	if _, err = mRes.ReadFrom(m.reader()); err != nil {
		t.Fatal(err)
	}
	ip, port, err := m.GetXORMappedAddress()
	if err != nil {
		t.Fatal(err)
	}
	if !ip.Equal(expectedIP) {
		t.Error("bad ip", ip, "!=", expectedIP)
	}
	if port != expectedPort {
		t.Error("bad port", port, "!=", expectedPort)
	}
}

func TestMessage_AddXORMappedAddressV6(t *testing.T) {
	m := New()
	transactionID, err := base64.StdEncoding.DecodeString("jxhBARZwX+rsC6er")
	if err != nil {
		t.Error(err)
	}
	copy(m.TransactionID[:], transactionID)
	expectedIP := net.ParseIP("fe80::dc2b:44ff:fe20:6009")
	expectedPort := 21254
	m.AddXORMappedAddress(expectedIP, expectedPort)
	m.WriteHeader()

	mRes := New()
	if _, err = mRes.ReadFrom(m.reader()); err != nil {
		t.Fatal(err)
	}
	ip, port, err := m.GetXORMappedAddress()
	if err != nil {
		t.Fatal(err)
	}
	if !ip.Equal(expectedIP) {
		t.Error("bad ip", ip, "!=", expectedIP)
	}
	if port != expectedPort {
		t.Error("bad port", port, "!=", expectedPort)
	}
}

func BenchmarkMessage_AddErrorCode(b *testing.B) {
	m := New()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.AddErrorCode(404, "Not found")
		m.Reset()
	}
}

func TestMessage_AddErrorCode(t *testing.T) {
	m := New()
	transactionID, err := base64.StdEncoding.DecodeString("jxhBARZwX+rsC6er")
	if err != nil {
		t.Error(err)
	}
	copy(m.TransactionID[:], transactionID)
	expectedCode := 404
	expectedReason := "Not found"
	m.AddErrorCode(expectedCode, expectedReason)
	m.WriteHeader()

	mRes := New()
	if _, err = mRes.ReadFrom(m.reader()); err != nil {
		t.Fatal(err)
	}
	code, reason, err := mRes.GetErrorCode()
	if err != nil {
		t.Error(err)
	}
	if code != expectedCode {
		t.Error("bad code", code)
	}
	if string(reason) != expectedReason {
		t.Error("bad reason", string(reason))
	}
}

func TestMessage_AddErrorCodeDefault(t *testing.T) {
	m := New()
	transactionID, err := base64.StdEncoding.DecodeString("jxhBARZwX+rsC6er")
	if err != nil {
		t.Error(err)
	}
	copy(m.TransactionID[:], transactionID)
	expectedCode := 500
	expectedReason := "Server Error"
	m.AddErrorCodeDefault(expectedCode)
	m.WriteHeader()

	mRes := New()
	if _, err = mRes.ReadFrom(m.reader()); err != nil {
		t.Fatal(err)
	}
	code, reason, err := mRes.GetErrorCode()
	if err != nil {
		t.Error(err)
	}
	if code != expectedCode {
		t.Error("bad code", code)
	}
	if string(reason) != expectedReason {
		t.Error("bad reason", string(reason))
	}
}
