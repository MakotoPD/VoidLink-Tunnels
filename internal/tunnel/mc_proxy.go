package tunnel

// Minecraft Java Edition TCP proxy.
// Listens on port 25565 (standard MC port), reads the handshake packet,
// extracts the server address (subdomain), finds the correct tunnel client,
// and relays the connection — including the already-read handshake bytes.
//
// Minecraft Handshake packet format (Java Edition, unencrypted):
//   [PacketLength: VarInt]
//   [PacketID:     VarInt = 0x00]
//   [Protocol:     VarInt]
//   [ServerAddr:   String = VarInt(len) + UTF-8 bytes]
//   [ServerPort:   UnsignedShort (2 bytes BE)]
//   [NextState:    VarInt  (1=status, 2=login)]

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

// startMCProxy starts the shared Minecraft TCP proxy.
func (s *Server) startMCProxy(ctx context.Context) {
	addr := fmt.Sprintf("0.0.0.0:%d", s.mcProxyPort)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[MCProxy] Failed to listen on %s: %v", addr, err)
		return
	}
	log.Printf("[MCProxy] Minecraft proxy listening on :%d (shared, routed by subdomain)", s.mcProxyPort)

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					time.Sleep(50 * time.Millisecond)
					continue
				}
			}
			go s.handleMCConnection(conn)
		}
	}()
}

func (s *Server) handleMCConnection(playerConn net.Conn) {
	defer playerConn.Close()

	serverAddr, buffered, err := parseMinecraftHandshake(playerConn)
	if err != nil {
		log.Printf("[MCProxy] Handshake parse error: %v", err)
		return
	}

	subdomain := extractSubdomainFromAddr(serverAddr, s.domain)
	if subdomain == "" {
		log.Printf("[MCProxy] Could not extract subdomain from %q", serverAddr)
		return
	}

	tunnelIDRaw, ok := s.subdomainMap.Load(subdomain)
	if !ok {
		log.Printf("[MCProxy] No tunnel for subdomain %q", subdomain)
		return
	}
	tunnelID := tunnelIDRaw.(string)

	clientRaw, ok := s.clients.Load(tunnelID)
	if !ok {
		log.Printf("[MCProxy] No client connected for tunnel %s (subdomain %s)", tunnelID, subdomain)
		return
	}
	client := clientRaw.(*ClientConn)

	mcPortRaw, _ := s.tunnelMCPort.LoadOrStore(tunnelID, 25565)
	mcPort := mcPortRaw.(int)

	connID := generateID()
	dataCh := make(chan net.Conn, 1)
	client.pendingTCP.Store(connID, dataCh)
	defer client.pendingTCP.Delete(connID)

	if err := client.send(fmt.Sprintf("OPEN %s %d", connID, mcPort)); err != nil {
		log.Printf("[MCProxy] Failed to send OPEN: %v", err)
		return
	}

	select {
	case dataConn := <-dataCh:
		defer dataConn.Close()
		// Prepend the buffered handshake bytes so the MC server sees the full packet
		dataConn.Write(buffered)
		relay(playerConn, dataConn)
	case <-time.After(dataConnTimeout):
		log.Printf("[MCProxy] Timeout waiting for data conn (tunnel %s)", tunnelID)
	}
}

// parseMinecraftHandshake reads and buffers the MC handshake packet.
// Returns the server address from the packet and all bytes read.
func parseMinecraftHandshake(conn net.Conn) (serverAddr string, readBytes []byte, err error) {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	raw := &bytes.Buffer{}
	r := io.TeeReader(conn, raw) // mirror everything read into raw

	readVarInt := func() (int, error) {
		var result, shift int
		for {
			b := make([]byte, 1)
			if _, e := io.ReadFull(r, b); e != nil {
				return 0, e
			}
			result |= int(b[0]&0x7F) << shift
			if b[0]&0x80 == 0 {
				return result, nil
			}
			shift += 7
			if shift >= 35 {
				return 0, fmt.Errorf("VarInt too large")
			}
		}
	}

	// Packet length
	pktLen, err := readVarInt()
	if err != nil || pktLen <= 0 || pktLen > 32768 {
		return "", raw.Bytes(), fmt.Errorf("bad packet length %d: %v", pktLen, err)
	}

	// Read entire packet body
	pktBody := make([]byte, pktLen)
	if _, err = io.ReadFull(r, pktBody); err != nil {
		return "", raw.Bytes(), err
	}

	// Parse packet body
	pr := bytes.NewReader(pktBody)
	readVarIntFrom := func(rd io.Reader) (int, error) {
		var result, shift int
		for {
			b := make([]byte, 1)
			if _, e := io.ReadFull(rd, b); e != nil {
				return 0, e
			}
			result |= int(b[0]&0x7F) << shift
			if b[0]&0x80 == 0 {
				return result, nil
			}
			shift += 7
			if shift >= 35 {
				return 0, fmt.Errorf("VarInt too large")
			}
		}
	}

	pktID, err := readVarIntFrom(pr)
	if err != nil || pktID != 0x00 {
		return "", raw.Bytes(), fmt.Errorf("expected handshake (0x00), got 0x%02X", pktID)
	}

	// Protocol version (discard)
	if _, err = readVarIntFrom(pr); err != nil {
		return "", raw.Bytes(), err
	}

	// Server address string
	strLen, err := readVarIntFrom(pr)
	if err != nil || strLen <= 0 || strLen > 255 {
		return "", raw.Bytes(), fmt.Errorf("bad server address length %d", strLen)
	}

	addrBytes := make([]byte, strLen)
	if _, err = io.ReadFull(pr, addrBytes); err != nil {
		return "", raw.Bytes(), err
	}

	serverAddr = string(addrBytes)

	// Strip BungeeCord / Forge null-byte suffixes
	if idx := strings.IndexByte(serverAddr, '\x00'); idx >= 0 {
		serverAddr = serverAddr[:idx]
	}

	// Strip trailing dot (some clients send "happy-cat.domain.com.")
	serverAddr = strings.TrimSuffix(serverAddr, ".")

	return serverAddr, raw.Bytes(), nil
}

// extractSubdomainFromAddr extracts the leftmost subdomain label from a full hostname.
// Example: domain="eu.domain.com", addr="happy-cat.eu.domain.com" → "happy-cat"
// Also handles addr="happy-cat.eu.domain.com:25565" (strips port).
func extractSubdomainFromAddr(addr, domain string) string {
	// Strip port if present
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	// Convert to lower case
	addr = strings.ToLower(addr)
	domain = strings.ToLower(domain)

	suffix := "." + domain
	if !strings.HasSuffix(addr, suffix) {
		// Try the addr itself (in case domain == addr base)
		// Fallback: just take everything before the first dot
		parts := strings.SplitN(addr, ".", 2)
		if len(parts) > 0 {
			return parts[0]
		}
		return ""
	}
	// Strip the domain suffix and the preceding dot
	prefix := strings.TrimSuffix(addr, suffix)
	// prefix might be "happy-cat" or "map.happy-cat"
	// Take the last component (the real subdomain label)
	parts := strings.Split(prefix, ".")
	return parts[len(parts)-1]
}
