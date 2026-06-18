// Package rollcall is a native Go client for the Grass Valley / Snell "RollCall"
// control protocol - the protocol that the Windows-only RollTrak.exe speaks.
//
// It exists to let Demeter (and its in-progress Go rewrite) talk to IQUCP / IQ
// frames directly over TCP, with no external binary and no Windows dependency.
//
// # Protocol
//
// RollCall is a framed binary request/response protocol over a single long-lived
// TCP connection to a frame/gateway (default port 2050). One connection
// multiplexes many units (cards): each message carries source and destination
// addresses, so Demeter needs only one Client per frame IP.
//
// Wire format (big-endian):
//
//	00 0C | outerLen(u16) | dst[6] | src[6] | innerLen(u16) | inner
//	inner: opcode | flags | cmdID(u32) | value
//	value: dataType(u32: 1=int, 2=string) | int(u32)  -- or, for strings --
//	       dataType(u32=2) | reserved(u32) | NUL-terminated ASCII
//
// Opcodes: 0x45 GET, 0x46 SET, 0x47 REPLY/NOTIFY (flags 0x80 = unsolicited),
// 0x1c OPEN, 0x01 ACK, 0x14/0x21 IDENTITY.
//
// The message codec (Message.Encode / Decode) is verified byte-for-byte against a
// real packet capture; see message_test.go. The session/handshake layer (OPEN,
// IDENTITY) and the RollTrak-CLI-to-Addr mapping are inferred and need
// confirmation against hardware. See the package README.
package rollcall
