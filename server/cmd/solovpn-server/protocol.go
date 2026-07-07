package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"sync/atomic"
)

const (
	packetMagic uint32 = 0x53565031
	typeControl byte   = 1
	typeData    byte   = 2

	aadSize   = 13
	nonceSize = 12
)

type PacketCodec struct {
	aead   cipher.AEAD
	prefix [4]byte
	seq    uint64
}

type DecodedPacket struct {
	Type    byte
	Payload []byte
}

func newPacketCodec(sharedKey string) (*PacketCodec, error) {
	key, err := base64.StdEncoding.DecodeString(sharedKey)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("shared key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	codec := &PacketCodec{aead: gcm}
	if _, err := rand.Read(codec.prefix[:]); err != nil {
		return nil, err
	}
	return codec, nil
}

func (c *PacketCodec) Seal(packetType byte, payload []byte) []byte {
	seq := atomic.AddUint64(&c.seq, 1)
	aad := make([]byte, aadSize)
	binary.BigEndian.PutUint32(aad[0:4], packetMagic)
	aad[4] = packetType
	binary.BigEndian.PutUint64(aad[5:13], seq)

	nonce := make([]byte, nonceSize)
	copy(nonce, c.prefix[:])
	binary.BigEndian.PutUint64(nonce[4:12], seq)

	out := make([]byte, 0, aadSize+nonceSize+len(payload)+c.aead.Overhead())
	out = append(out, aad...)
	out = append(out, nonce...)
	return c.aead.Seal(out, nonce, payload, aad)
}

func (c *PacketCodec) Open(packet []byte) (*DecodedPacket, bool) {
	if len(packet) <= aadSize+nonceSize+c.aead.Overhead() {
		return nil, false
	}
	if binary.BigEndian.Uint32(packet[0:4]) != packetMagic {
		return nil, false
	}

	packetType := packet[4]
	aad := packet[:aadSize]
	nonce := packet[aadSize : aadSize+nonceSize]
	encrypted := packet[aadSize+nonceSize:]
	payload, err := c.aead.Open(nil, nonce, encrypted, aad)
	if err != nil {
		return nil, false
	}
	return &DecodedPacket{Type: packetType, Payload: payload}, true
}
