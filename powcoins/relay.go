package powcoins

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
)

type rawP2PMessage struct {
	command string
	payload []byte
}

func RelayTx(peerAddr string, tx *wire.MsgTx, timeout time.Duration) error {
	if peerAddr == "" {
		peerAddr = DefaultRelayPeer
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if !strings.Contains(peerAddr, ":") {
		peerAddr += ":38333"
	}

	conn, err := net.DialTimeout("tcp", peerAddr, timeout)
	if err != nil {
		return fmt.Errorf("connect relay peer: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	remoteHost, remotePortString, err := net.SplitHostPort(peerAddr)
	if err != nil {
		return err
	}
	remotePort64, err := strconv.ParseUint(remotePortString, 10, 16)
	if err != nil {
		return err
	}
	remoteIPs, err := net.LookupIP(remoteHost)
	if err != nil {
		return err
	}
	remoteIP := remoteIPs[0]
	localIP := net.IPv4zero
	if localAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		localIP = localAddr.IP
	}

	pver := uint32(wire.ProtocolVersion)
	netID := chaincfg.SigNetParams.Net
	me := wire.NewNetAddressIPPort(localIP, 0, 0)
	you := wire.NewNetAddressIPPort(remoteIP, uint16(remotePort64), 0)
	version := wire.NewMsgVersion(me, you, rand.Uint64(), 0)
	_ = version.AddUserAgent("powcoins-go", "0.1.0")
	if _, err := wire.WriteMessageWithEncodingN(conn, version, pver, netID, wire.BaseEncoding); err != nil {
		return fmt.Errorf("send version: %w", err)
	}

	verackReceived := false
	versionReceived := false
	for !(verackReceived && versionReceived) {
		msg, err := readRawMessage(conn, netID)
		if err != nil {
			return fmt.Errorf("handshake read: %w", err)
		}
		switch msg.command {
		case wire.CmdVersion:
			var version wire.MsgVersion
			if err := version.BtcDecode(bytes.NewBuffer(msg.payload), pver, wire.BaseEncoding); err != nil {
				return fmt.Errorf("decode version: %w", err)
			}
			versionReceived = true
			if _, err := wire.WriteMessageWithEncodingN(conn, wire.NewMsgVerAck(), pver, netID, wire.BaseEncoding); err != nil {
				return fmt.Errorf("send verack: %w", err)
			}
		case wire.CmdVerAck:
			verackReceived = true
		case wire.CmdPing:
			if err := handlePing(conn, msg.payload, pver, netID); err != nil {
				return err
			}
		}
	}

	txHash := tx.TxHash()
	inv := wire.NewMsgInvSizeHint(1)
	if err := inv.AddInvVect(wire.NewInvVect(wire.InvTypeTx, &txHash)); err != nil {
		return err
	}
	if _, err := wire.WriteMessageWithEncodingN(conn, inv, pver, netID, wire.BaseEncoding); err != nil {
		return fmt.Errorf("send inv: %w", err)
	}

	for {
		msg, err := readRawMessage(conn, netID)
		if err != nil {
			return fmt.Errorf("wait getdata: %w", err)
		}
		if msg.command == wire.CmdPing {
			if err := handlePing(conn, msg.payload, pver, netID); err != nil {
				return err
			}
			continue
		}
		if msg.command != wire.CmdGetData {
			continue
		}
		var getData wire.MsgGetData
		if err := getData.BtcDecode(bytes.NewReader(msg.payload), pver, wire.WitnessEncoding); err != nil {
			return fmt.Errorf("decode getdata: %w", err)
		}
		for _, invVect := range getData.InvList {
			if (invVect.Type == wire.InvTypeTx || invVect.Type == wire.InvTypeWitnessTx) &&
				invVect.Hash.IsEqual(&txHash) {

				if _, err := wire.WriteMessageWithEncodingN(conn, tx, pver, netID, wire.WitnessEncoding); err != nil {
					return fmt.Errorf("send tx: %w", err)
				}
				return nil
			}
		}
	}
}

func readRawMessage(r io.Reader, netID wire.BitcoinNet) (rawP2PMessage, error) {
	var header [wire.MessageHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return rawP2PMessage{}, err
	}

	magic := wire.BitcoinNet(binaryLittleEndian(header[0:4]))
	if magic != netID {
		return rawP2PMessage{}, fmt.Errorf("message from other network %v", magic)
	}

	command := strings.TrimRight(string(header[4:16]), "\x00")
	length := binaryLittleEndian(header[16:20])
	if length > wire.MaxMessagePayload {
		return rawP2PMessage{}, fmt.Errorf("payload too large: %d", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return rawP2PMessage{}, err
	}
	return rawP2PMessage{command: command, payload: payload}, nil
}

func handlePing(conn net.Conn, payload []byte, pver uint32, netID wire.BitcoinNet) error {
	var ping wire.MsgPing
	if err := ping.BtcDecode(bytes.NewReader(payload), pver, wire.BaseEncoding); err != nil {
		return fmt.Errorf("decode ping: %w", err)
	}
	if _, err := wire.WriteMessageWithEncodingN(conn, wire.NewMsgPong(ping.Nonce), pver, netID, wire.BaseEncoding); err != nil {
		return fmt.Errorf("send pong: %w", err)
	}
	return nil
}

func binaryLittleEndian(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
